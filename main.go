package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	_ "modernc.org/sqlite"
)

// Models
type Store struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Owner    string  `json:"owner"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	AdminPIN string  `json:"admin_pin"`
	APIKey   string  `json:"api_key"`
	IsActive int     `json:"is_active"`
}

type Product struct {
	ID         int    `json:"id"`
	StoreID    int    `json:"store_id"`
	Name       string `json:"name"`
	Emoji      string `json:"emoji"`
	Price      int    `json:"price"`
	Unit       string `json:"unit"`
	Cat        string `json:"cat"`
	Stock      int    `json:"stock"`
	StockAlert int    `json:"stock_alert"`
}

type OrderRequest struct {
	StoreID   int                `json:"store_id"`
	Items     []OrderItemRequest `json:"items"`
	Total     int                `json:"total"`
	Discount  int                `json:"discount"`
	PayMethod string             `json:"pay_method"`
}

type OrderItemRequest struct {
	ProductID int    `json:"product_id"`
	Name      string `json:"name"`
	Price     int    `json:"price"`
	Qty       int    `json:"qty"`
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite", "./pentamarket.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	initDB()

	app := fiber.New()
	app.Use(logger.New())
	app.Use(cors.New())

	// Static files
	app.Static("/", "./")

	// Middleware to get Store-ID via API Key
	getStoreID := func(c *fiber.Ctx) int {
		apiKey := c.Get("X-API-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}
		if apiKey == "" {
			return 0
		}
		var storeID int
		err := db.QueryRow("SELECT id FROM stores WHERE api_key = ?", apiKey).Scan(&storeID)
		if err != nil {
			return 0
		}
		return storeID
	}

	// API Routes
	api := app.Group("/api")

	// Stores & Auth
	api.Get("/stores", func(c *fiber.Ctx) error {
		// Only show active stores to general users
		rows, _ := db.Query("SELECT id, name, owner, lat, lng FROM stores WHERE is_active = 1")
		var stores []Store
		for rows.Next() {
			var s Store
			rows.Scan(&s.ID, &s.Name, &s.Owner, &s.Lat, &s.Lng)
			stores = append(stores, s)
		}
		return c.JSON(stores)
	})

	api.Post("/stores/verify-pin", func(c *fiber.Ctx) error {
		var req struct {
			StoreID int    `json:"store_id"`
			PIN     string `json:"pin"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).SendString("Invalid request")
		}

		var correctPIN string
		err := db.QueryRow("SELECT admin_pin FROM stores WHERE id = ?", req.StoreID).Scan(&correctPIN)
		if err != nil {
			return c.Status(404).SendString("Store not found")
		}

		if req.PIN == correctPIN {
			return c.JSON(fiber.Map{"success": true})
		}
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Mã PIN không đúng"})
	})

	api.Post("/stores", func(c *fiber.Ctx) error {
		var req struct {
			Store
			RegKey string `json:"reg_key"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).SendString("Invalid data")
		}

		const MASTER_KEY = "PENTA2026"
		if req.RegKey != MASTER_KEY {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "Mã kích hoạt hệ thống không đúng"})
		}

		// Generate Random API Key
		randomKey := fmt.Sprintf("PENTA-%X", time.Now().UnixNano()%0xFFFFFF)
		req.APIKey = randomKey

		if req.AdminPIN == "" { req.AdminPIN = "1234" }
		res, err := db.Exec("INSERT INTO stores (name, owner, lat, lng, admin_pin, api_key, is_active) VALUES (?, ?, ?, ?, ?, ?, 1)", 
			req.Name, req.Owner, req.Lat, req.Lng, req.AdminPIN, req.APIKey)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		id, _ := res.LastInsertId()
		req.ID = int(id)
		return c.JSON(req)
	})

	api.Post("/stores/verify-key", func(c *fiber.Ctx) error {
		var req struct { APIKey string `json:"api_key"`; StoreID int `json:"store_id"` }
		c.BodyParser(&req)
		var s Store
		err := db.QueryRow("SELECT id, name FROM stores WHERE api_key = ? AND id = ?", req.APIKey, req.StoreID).Scan(&s.ID, &s.Name)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "store": s})
	})

	api.Get("/system/stores/all", func(c *fiber.Ctx) error {
		pin := c.Query("super_pin")
		if pin != "9999" {
			return c.Status(401).SendString("Unauthorized")
		}
		rows, _ := db.Query("SELECT id, name, owner, lat, lng, is_active FROM stores")
		var stores []interface{}
		for rows.Next() {
			var s struct {
				ID       int     `json:"id"`
				Name     string  `json:"name"`
				Owner    string  `json:"owner"`
				Lat      float64 `json:"lat"`
				Lng      float64 `json:"lng"`
				IsActive int     `json:"is_active"`
			}
			rows.Scan(&s.ID, &s.Name, &s.Owner, &s.Lat, &s.Lng, &s.IsActive)
			stores = append(stores, s)
		}
		return c.JSON(stores)
	})

	api.Post("/system/stores/approve", func(c *fiber.Ctx) error {
		pin := c.Query("super_pin")
		if pin != "9999" { return c.Status(401).SendString("Unauthorized") }
		var req struct { ID int `json:"id"` }
		c.BodyParser(&req)
		db.Exec("UPDATE stores SET is_active = 1 WHERE id = ?", req.ID)
		return c.JSON(fiber.Map{"success": true})
	})

	api.Delete("/system/stores/:id", func(c *fiber.Ctx) error {
		pin := c.Query("super_pin")
		if pin != "9999" { return c.Status(401).SendString("Unauthorized") }
		db.Exec("DELETE FROM stores WHERE id = ?", c.Params("id"))
		return c.JSON(fiber.Map{"success": true})
	})

	api.Get("/system/stats", func(c *fiber.Ctx) error {
		// Basic Super Admin Auth (simple PIN for now)
		pin := c.Query("super_pin")
		if pin != "9999" { // Default Super PIN
			return c.Status(401).SendString("Unauthorized")
		}

		var stats struct {
			TotalStores  int `json:"total_stores"`
			TotalOrders  int `json:"total_orders"`
			TotalRevenue int `json:"total_revenue"`
		}
		db.QueryRow("SELECT COUNT(*) FROM stores").Scan(&stats.TotalStores)
		db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&stats.TotalOrders)
		db.QueryRow("SELECT SUM(total) FROM orders").Scan(&stats.TotalRevenue)
		
		return c.JSON(stats)
	})

	// Products
	api.Get("/products", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		rows, err := db.Query("SELECT id, name, COALESCE(emoji, ''), price, unit, cat, stock, stock_alert FROM products WHERE store_id = ?", storeID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer rows.Close()

		var products []Product
		for rows.Next() {
			var p Product
			p.StoreID = storeID
			rows.Scan(&p.ID, &p.Name, &p.Emoji, &p.Price, &p.Unit, &p.Cat, &p.Stock, &p.StockAlert)
			products = append(products, p)
		}
		return c.JSON(products)
	})

	api.Post("/products", func(c *fiber.Ctx) error {
		var p Product
		if err := c.BodyParser(&p); err != nil {
			return c.Status(400).SendString(err.Error())
		}
		storeID := getStoreID(c)
		if p.StoreID == 0 {
			p.StoreID = storeID
		}

		if p.ID > 0 {
			_, err := db.Exec("UPDATE products SET name=?, emoji=?, price=?, unit=?, cat=?, stock=?, stock_alert=? WHERE id=? AND store_id=?",
				p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock, p.StockAlert, p.ID, p.StoreID)
			if err != nil {
				return c.Status(500).SendString(err.Error())
			}
		} else {
			res, err := db.Exec("INSERT INTO products (store_id, name, emoji, price, unit, cat, stock, stock_alert) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
				p.StoreID, p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock, p.StockAlert)
			if err != nil {
				return c.Status(500).SendString(err.Error())
			}
			id, _ := res.LastInsertId()
			p.ID = int(id)
		}
		return c.JSON(p)
	})

	api.Delete("/products/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		storeID := getStoreID(c)
		_, err := db.Exec("DELETE FROM products WHERE id=? AND store_id=?", id, storeID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"message": "Deleted"})
	})

	// Orders
	api.Post("/orders", func(c *fiber.Ctx) error {
		var body OrderRequest
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).SendString(err.Error())
		}
		storeID := getStoreID(c)
		if body.StoreID == 0 {
			body.StoreID = storeID
		}

		tx, err := db.Begin()
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		res, err := tx.Exec("INSERT INTO orders (store_id, total, discount, pay_method) VALUES (?, ?, ?, ?)",
			body.StoreID, body.Total, body.Discount, body.PayMethod)
		if err != nil {
			tx.Rollback()
			return c.Status(500).SendString(err.Error())
		}

		orderID, _ := res.LastInsertId()

		for _, item := range body.Items {
			_, err = tx.Exec("INSERT INTO order_items (order_id, product_id, product_name, price, qty) VALUES (?, ?, ?, ?, ?)",
				orderID, item.ProductID, item.Name, item.Price, item.Qty)
			if err != nil {
				tx.Rollback()
				return c.Status(500).SendString(err.Error())
			}

			// Deduct stock
			_, err = tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ? AND store_id = ?", item.Qty, item.ProductID, body.StoreID)
			if err != nil {
				tx.Rollback()
				return c.Status(500).SendString(err.Error())
			}
		}

		tx.Commit()
		return c.JSON(fiber.Map{"id": orderID})
	})

	api.Get("/orders", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		date := c.Query("date")
		query := "SELECT id, total, discount, pay_method, created_at FROM orders WHERE store_id = ?"
		var args = []interface{}{storeID}

		if date == "today" {
			query += " AND DATE(created_at) = DATE('now')"
		} else if date != "" {
			query += " AND DATE(created_at) = ?"
			args = append(args, date)
		}
		query += " ORDER BY created_at DESC"

		rows, err := db.Query(query, args...)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer rows.Close()

		var orders []fiber.Map
		for rows.Next() {
			var id, total, discount int
			var payMethod, createdAt string
			rows.Scan(&id, &total, &discount, &payMethod, &createdAt)

			itemRows, _ := db.Query("SELECT product_name, price, qty FROM order_items WHERE order_id = ?", id)
			var items []fiber.Map
			for itemRows.Next() {
				var name string
				var price, qty int
				itemRows.Scan(&name, &price, &qty)
				items = append(items, fiber.Map{"name": name, "price": price, "qty": qty})
			}
			itemRows.Close()

			orders = append(orders, fiber.Map{
				"id":         id,
				"total":      total,
				"discount":   discount,
				"pay_method": payMethod,
				"created_at": createdAt,
				"items":      items,
			})
		}
		return c.JSON(orders)
	})

	// Reports
	api.Get("/reports/daily", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		rows, err := db.Query(`SELECT DATE(created_at) as day, SUM(total) as revenue 
			FROM orders WHERE store_id = ? AND created_at >= date('now','-30 days') 
			GROUP BY day ORDER BY day ASC`, storeID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer rows.Close()

		var result []fiber.Map
		for rows.Next() {
			var day string
			var revenue int
			rows.Scan(&day, &revenue)
			result = append(result, fiber.Map{"day": day, "revenue": revenue})
		}
		return c.JSON(result)
	})

	api.Get("/reports/top-products", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		rows, err := db.Query(`SELECT product_name, SUM(qty) as sold 
			FROM order_items i JOIN orders o ON i.order_id = o.id 
			WHERE o.store_id = ? AND o.created_at >= date('now','-30 days') 
			GROUP BY product_name ORDER BY sold DESC LIMIT 5`, storeID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer rows.Close()

		var result []fiber.Map
		for rows.Next() {
			var name string
			var sold int
			rows.Scan(&name, &sold)
			result = append(result, fiber.Map{"name": name, "sold": sold})
		}
		return c.JSON(result)
	})

	api.Get("/reports/payment-stats", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		rows, err := db.Query(`SELECT pay_method, COUNT(*) as count, SUM(total) as revenue 
			FROM orders WHERE store_id = ? GROUP BY pay_method`, storeID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer rows.Close()

		var result []fiber.Map
		for rows.Next() {
			var method string
			var count, revenue int
			rows.Scan(&method, &count, &revenue)
			result = append(result, fiber.Map{"method": method, "count": count, "revenue": revenue})
		}
		return c.JSON(result)
	})

	// Inventory
	api.Post("/inventory/restock", func(c *fiber.Ctx) error {
		var body struct {
			ProductID   int    `json:"product_id"`
			Qty         int    `json:"qty"`
			CostPerUnit int    `json:"cost_per_unit"`
			Note        string `json:"note"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).SendString(err.Error())
		}
		storeID := getStoreID(c)

		tx, err := db.Begin()
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ? AND store_id = ?", body.Qty, body.ProductID, storeID)
		if err != nil {
			tx.Rollback()
			return c.Status(500).SendString(err.Error())
		}

		_, err = tx.Exec("INSERT INTO restock_log (product_id, qty_added, cost_per_unit, note) VALUES (?, ?, ?, ?)",
			body.ProductID, body.Qty, body.CostPerUnit, body.Note)
		if err != nil {
			tx.Rollback()
			return c.Status(500).SendString(err.Error())
		}

		tx.Commit()
		return c.JSON(fiber.Map{"message": "Restocked"})
	})

	api.Post("/reset", func(c *fiber.Ctx) error {
		storeID := getStoreID(c)
		db.Exec("DELETE FROM products WHERE store_id = ?", storeID)
		db.Exec("DELETE FROM orders WHERE store_id = ?", storeID)
		return c.JSON(fiber.Map{"message": "Reset successful"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(app.Listen(":" + port))
}

func initDB() {
	// Create stores table first
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS stores (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		owner TEXT NOT NULL,
		lat REAL DEFAULT 0,
		lng REAL DEFAULT 0,
		admin_pin TEXT DEFAULT '1234',
		api_key TEXT,
		is_active INTEGER DEFAULT 0
	);`)

	// Migration: Add new columns if missing
	_, _ = db.Exec("ALTER TABLE stores ADD COLUMN lat REAL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE stores ADD COLUMN lng REAL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE stores ADD COLUMN admin_pin TEXT DEFAULT '1234'")
	_, _ = db.Exec("ALTER TABLE stores ADD COLUMN is_active INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE stores ADD COLUMN api_key TEXT")
	// Legacy update
	db.Exec("UPDATE stores SET is_active = 1 WHERE is_active IS NULL")
	db.Exec("UPDATE stores SET api_key = 'KEY1' WHERE id = 1 AND api_key IS NULL")
	db.Exec("UPDATE stores SET api_key = 'KEY2' WHERE id = 2 AND api_key IS NULL")

	// Ensure default stores exist
	var storeCount int
	db.QueryRow("SELECT COUNT(*) FROM stores").Scan(&storeCount)
	if storeCount == 0 {
		// Mock coordinates for 1km testing (around 10.762622, 106.660172 in Saigon)
		db.Exec("INSERT INTO stores (id, name, owner, lat, lng, admin_pin) VALUES (1, 'Tạp Hoá Thuý Nga', 'Chị Thuý', 10.7626, 106.6601, '1111')")
		db.Exec("INSERT INTO stores (id, name, owner, lat, lng, admin_pin) VALUES (2, 'Tiệm Tạp Hoá 247', 'Anh Thành', 10.7650, 106.6650, '2222')")
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS products (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			store_id INTEGER DEFAULT 1,
			name TEXT NOT NULL,
			emoji TEXT,
			price INTEGER NOT NULL,
			unit TEXT,
			cat TEXT,
			stock INTEGER DEFAULT 0,
			stock_alert INTEGER DEFAULT 5
		);`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			store_id INTEGER DEFAULT 1,
			total INTEGER NOT NULL,
			discount INTEGER DEFAULT 0,
			pay_method TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS order_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id INTEGER REFERENCES orders(id),
			product_id INTEGER,
			product_name TEXT,
			price INTEGER,
			qty INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS restock_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			product_id INTEGER,
			qty_added INTEGER,
			cost_per_unit INTEGER,
			note TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		db.Exec(q)
	}

	// Migration: Add store_id if it doesn't exist (SQLite style)
	// We use a safe check by trying to add and ignoring error
	_, _ = db.Exec("ALTER TABLE products ADD COLUMN store_id INTEGER DEFAULT 1")
	_, _ = db.Exec("ALTER TABLE orders ADD COLUMN store_id INTEGER DEFAULT 1")

	// Seed if empty
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM products WHERE store_id = 1").Scan(&count)
	if err == nil && count == 0 {
		seedProducts(1)
	}
}

func seedProducts(storeID int) {
	products := []Product{
		{Name: "Nước suối Aquafina", Emoji: "💧", Price: 6000, Unit: "chai", Cat: "Đồ uống", Stock: 50},
		{Name: "Pepsi lon 330ml", Emoji: "🥤", Price: 12000, Unit: "lon", Cat: "Đồ uống", Stock: 24},
		{Name: "Trà Ô Long 500ml", Emoji: "🍵", Price: 15000, Unit: "chai", Cat: "Đồ uống", Stock: 15},
		{Name: "Mì Hảo Hảo", Emoji: "🍜", Price: 5000, Unit: "gói", Cat: "Mì - Cháo", Stock: 100},
	}
	for _, p := range products {
		db.Exec("INSERT INTO products (store_id, name, emoji, price, unit, cat, stock) VALUES (?, ?, ?, ?, ?, ?, ?)",
			storeID, p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock)
	}
}
