package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// --- CONFIGURATION ---
const (
	MinPort     = 20000
	MaxPort     = 21000
	Domain      = "eu.yourdomain.com"
	DBName      = "./tunnels.db"
	SecretKey   = "highly_secret_key_for_signing_tokens_change_to" // IMPORTANT: Keep in environment variables in production!
	MaxTunnels  = 2 // Tunnel limit per user
)

// --- DATA STRUCTURES ---

// Data for registration and login
type AuthRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Response for allocation
type AllocationResponse struct {
	ServerAddress string `json:"server_address"`
	PublicPort    int    `json:"public_port"`
	FullAddress   string `json:"full_address"`
	Subdomain     string `json:"subdomain"`
}

// JWT Claims structure (encrypted in the token)
type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

var db *sql.DB

// --- MAIN FUNCTION ---

func main() {
	initDB()
	defer db.Close()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 1. Public Endpoints (Available to everyone)
	r.POST("/api/register", registerHandler)
	r.POST("/api/login", loginHandler)
	
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	// 2. Protected Endpoints Group (Require login)
	protected := r.Group("/api")
	protected.Use(authMiddleware()) // Inserting "bouncer" here
	{
		protected.POST("/allocate", allocateHandler)
		protected.GET("/my-tunnels", myTunnelsHandler) // New: show my tunnels
	}

	fmt.Println("Auth System running on port 8080")
	r.Run(":8080")
}

// --- DATABASE LOGIC ---

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", DBName)
	if err != nil {
		log.Fatal(err)
	}

	// Users Table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL
	);`)
	if err != nil { log.Fatal(err) }

	// Tunnels Table (now linked to user ID)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tunnels (
		port INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);`)
	if err != nil { log.Fatal(err) }
}

// --- HANDLERS (Request handling) ---

// Registration
func registerHandler(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	// Password hashing (never save plain text!)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return
	}

	// Save to DB
	_, err = db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", req.Username, string(hashedPassword))
	if err != nil {
		// Check if error is "user already exists" (UNIQUE constraint)
		if strings.Contains(err.Error(), "UNIQUE") {
			c.JSON(http.StatusConflict, gin.H{"error": "User with this name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Account created successfully"})
}

// Login
func loginHandler(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	// Get user from DB
	var id int
	var hash string
	err := db.QueryRow("SELECT id, password_hash FROM users WHERE username = ?", req.Username).Scan(&id, &hash)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid login or password"})
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid login or password"})
		return
	}

	// ID Token Generation
	expirationTime := time.Now().Add(24 * time.Hour) // Token valid for 24h
	claims := &Claims{
		UserID: id,
		Username: req.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(SecretKey))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": tokenString})
}

// Tunnel allocation (Available only for logged in users)
func allocateHandler(c *gin.Context) {
	// Get user ID from context (set by Middleware)
	userID, _ := c.Get("userID") // "casting" to int

	// 1. Check tunnel limit
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tunnels WHERE user_id = ?", userID).Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if count >= MaxTunnels {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Tunnel limit reached (%d)", MaxTunnels)})
		return
	}

	// 2. Search for free port
	allocatedPort, err := findAndReservePort(userID.(int))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No free ports available!"})
		return
	}

	// 3. Response
	subdomain := fmt.Sprintf("srv-%d", allocatedPort)
	resp := AllocationResponse{
		ServerAddress: Domain,
		PublicPort:    allocatedPort,
		Subdomain:     subdomain,
		FullAddress:   fmt.Sprintf("%s.%s", subdomain, Domain),
	}
	c.JSON(http.StatusOK, resp)
}

// User tunnels list
func myTunnelsHandler(c *gin.Context) {
	userID, _ := c.Get("userID")

	rows, err := db.Query("SELECT port FROM tunnels WHERE user_id = ?", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	defer rows.Close()

	var ports []int
	for rows.Next() {
		var p int
		rows.Scan(&p)
		ports = append(ports, p)
	}

	c.JSON(http.StatusOK, gin.H{"tunnels": ports})
}

// --- MIDDLEWARE (BOUNCER) ---

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Login required"})
			c.Abort()
			return
		}

		// Expecting header: "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token format"})
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Save user data in context for subsequent functions
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// Helper function for DB reservation
func findAndReservePort(userID int) (int, error) {
	for port := MinPort; port <= MaxPort; port++ {
		_, err := db.Exec("INSERT INTO tunnels (port, user_id) VALUES (?, ?)", port, userID)
		if err == nil {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no spots available")
}