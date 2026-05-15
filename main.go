package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO needed)
)

// Models
type Product struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Emoji      string `json:"emoji"`
	Price      int    `json:"price"`
	Unit       string `json:"unit"`
	Cat        string `json:"cat"`
	Stock      int    `json:"stock"`
	StockAlert int    `json:"stock_alert"`
}

type OrderRequest struct {
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

	// API Routes
	api := app.Group("/api")

	// Products
	api.Get("/products", getProducts)
	api.Post("/products", saveProduct)
	api.Delete("/products/:id", deleteProduct)

	// Orders
	api.Post("/orders", checkout)
	api.Get("/orders", getOrders)

	// Reports
	api.Get("/reports/daily", getDailyReport)
	api.Get("/reports/top-products", getTopProducts)
	api.Get("/reports/payment-stats", getPaymentStats)

	// Inventory
	api.Post("/inventory/restock", restock)
	api.Post("/reset", resetDB)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(app.Listen(":" + port))
}

func initDB() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS products (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		_, err := db.Exec(q)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Seed if empty
	var count int
	db.QueryRow("SELECT COUNT(*) FROM products").Scan(&count)
	if count == 0 {
		seedProducts()
	}
}

func seedProducts() {
	products := []Product{
		{Name: "Nước suối Aquafina", Emoji: "💧", Price: 6000, Unit: "chai", Cat: "Đồ uống", Stock: 50},
		{Name: "Pepsi lon 330ml", Emoji: "🥤", Price: 12000, Unit: "lon", Cat: "Đồ uống", Stock: 24},
		{Name: "Trà Ô Long 500ml", Emoji: "🍵", Price: 15000, Unit: "chai", Cat: "Đồ uống", Stock: 15},
		{Name: "Mì Hảo Hảo", Emoji: "🍜", Price: 5000, Unit: "gói", Cat: "Mì - Cháo", Stock: 100},
	}
	for _, p := range products {
		db.Exec("INSERT INTO products (name, emoji, price, unit, cat, stock) VALUES (?, ?, ?, ?, ?, ?)",
			p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock)
	}
}

// Handlers
func getProducts(c *fiber.Ctx) error {
	rows, err := db.Query("SELECT id, name, emoji, price, unit, cat, stock, stock_alert FROM products")
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		rows.Scan(&p.ID, &p.Name, &p.Emoji, &p.Price, &p.Unit, &p.Cat, &p.Stock, &p.StockAlert)
		products = append(products, p)
	}
	return c.JSON(products)
}

func saveProduct(c *fiber.Ctx) error {
	var p Product
	if err := c.BodyParser(&p); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	if p.ID > 0 {
		_, err := db.Exec("UPDATE products SET name=?, emoji=?, price=?, unit=?, cat=?, stock=?, stock_alert=? WHERE id=?",
			p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock, p.StockAlert, p.ID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
	} else {
		res, err := db.Exec("INSERT INTO products (name, emoji, price, unit, cat, stock, stock_alert) VALUES (?, ?, ?, ?, ?, ?, ?)",
			p.Name, p.Emoji, p.Price, p.Unit, p.Cat, p.Stock, p.StockAlert)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		id, _ := res.LastInsertId()
		p.ID = int(id)
	}
	return c.JSON(p)
}

func deleteProduct(c *fiber.Ctx) error {
	id := c.Params("id")
	_, err := db.Exec("DELETE FROM products WHERE id=?", id)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	return c.JSON(fiber.Map{"message": "Deleted"})
}

func checkout(c *fiber.Ctx) error {
	var body OrderRequest
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	tx, err := db.Begin()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	res, err := tx.Exec("INSERT INTO orders (total, discount, pay_method) VALUES (?, ?, ?)",
		body.Total, body.Discount, body.PayMethod)
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
		_, err = tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", item.Qty, item.ProductID)
		if err != nil {
			tx.Rollback()
			return c.Status(500).SendString(err.Error())
		}
	}

	tx.Commit()
	return c.JSON(fiber.Map{"id": orderID})
}

func getOrders(c *fiber.Ctx) error {
	date := c.Query("date")
	query := "SELECT id, total, discount, pay_method, created_at FROM orders"
	var args []interface{}

	if date == "today" {
		query += " WHERE DATE(created_at) = DATE('now')"
	} else if date != "" {
		query += " WHERE DATE(created_at) = ?"
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

		// Get items for each order
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
}

func getDailyReport(c *fiber.Ctx) error {
	rows, err := db.Query(`SELECT DATE(created_at) as day, SUM(total) as revenue 
		FROM orders WHERE created_at >= date('now','-30 days') 
		GROUP BY day ORDER BY day ASC`)
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
}

func getTopProducts(c *fiber.Ctx) error {
	rows, err := db.Query(`SELECT product_name, SUM(qty) as sold 
		FROM order_items i JOIN orders o ON i.order_id = o.id 
		WHERE o.created_at >= date('now','-30 days') 
		GROUP BY product_name ORDER BY sold DESC LIMIT 5`)
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
}

func getPaymentStats(c *fiber.Ctx) error {
	rows, err := db.Query(`SELECT pay_method, COUNT(*) as count, SUM(total) as revenue 
		FROM orders GROUP BY pay_method`)
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
}

func restock(c *fiber.Ctx) error {
	var body struct {
		ProductID   int    `json:"product_id"`
		Qty         int    `json:"qty"`
		CostPerUnit int    `json:"cost_per_unit"`
		Note        string `json:"note"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	tx, err := db.Begin()
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", body.Qty, body.ProductID)
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
}

func resetDB(c *fiber.Ctx) error {
	_, err := db.Exec("DELETE FROM products")
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	_, err = db.Exec("DELETE FROM sqlite_sequence WHERE name='products'")
	if err != nil {
		log.Println("Error resetting sequence:", err)
	}
	seedProducts()
	return c.JSON(fiber.Map{"message": "Reset successful"})
}
