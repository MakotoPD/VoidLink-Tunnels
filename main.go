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

// --- KONFIGURACJA ---
const (
	MinPort     = 20000
	MaxPort     = 21000
	Domain      = "eu.twojadomena.com"
	DBName      = "./tunnels.db"
	SecretKey   = "bardzo_tajny_klucz_do_podpisywania_tokenow_zmien_to" // WAŻNE: W produkcji trzymaj w zmiennych środowiskowych!
	MaxTunnels  = 2 // Limit tuneli na jednego użytkownika
)

// --- STRUKTURY DANYCH ---

// Dane do rejestracji i logowania
type AuthRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Odpowiedź przy alokacji
type AllocationResponse struct {
	ServerAddress string `json:"server_address"`
	PublicPort    int    `json:"public_port"`
	FullAddress   string `json:"full_address"`
	Subdomain     string `json:"subdomain"`
}

// Struktura tokenu JWT (to co jest zaszyfrowane w tokenie)
type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

var db *sql.DB

// --- GŁÓWNA FUNKCJA ---

func main() {
	initDB()
	defer db.Close()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 1. Endpointy Publiczne (Dostępne dla każdego)
	r.POST("/api/register", registerHandler)
	r.POST("/api/login", loginHandler)
	
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	// 2. Grupa Endpointów Chronionych (Wymagają zalogowania)
	protected := r.Group("/api")
	protected.Use(authMiddleware()) // Tutaj wstawiamy "ochroniarza"
	{
		protected.POST("/allocate", allocateHandler)
		protected.GET("/my-tunnels", myTunnelsHandler) // Nowość: pokaż moje tunele
	}

	fmt.Println("System z Autoryzacją działa na porcie 8080")
	r.Run(":8080")
}

// --- LOGIKA BAZY DANYCH ---

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", DBName)
	if err != nil {
		log.Fatal(err)
	}

	// Tabela Użytkowników
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL
	);`)
	if err != nil { log.Fatal(err) }

	// Tabela Tuneli (teraz powiązana z ID użytkownika)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tunnels (
		port INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);`)
	if err != nil { log.Fatal(err) }
}

// --- HANDLERY (Obsługa zapytań) ---

// Rejestracja
func registerHandler(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Błędne dane"})
		return
	}

	// Hashowanie hasła (nigdy nie zapisujemy czystego tekstu!)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Błąd serwera"})
		return
	}

	// Zapis do bazy
	_, err = db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", req.Username, string(hashedPassword))
	if err != nil {
		// Sprawdzenie czy błąd to "użytkownik już istnieje" (UNIQUE constraint)
		if strings.Contains(err.Error(), "UNIQUE") {
			c.JSON(http.StatusConflict, gin.H{"error": "Użytkownik o takiej nazwie już istnieje"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Błąd bazy danych"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Konto utworzone pomyślnie"})
}

// Logowanie
func loginHandler(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Błędne dane"})
		return
	}

	// Pobranie użytkownika z bazy
	var id int
	var hash string
	err := db.QueryRow("SELECT id, password_hash FROM users WHERE username = ?", req.Username).Scan(&id, &hash)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Błędny login lub hasło"})
		return
	}

	// Sprawdzenie hasła
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Błędny login lub hasło"})
		return
	}

	// Generowanie Tokenu JWT
	expirationTime := time.Now().Add(24 * time.Hour) // Token ważny 24h
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Nie udało się wygenerować tokenu"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": tokenString})
}

// Tworzenie tunelu (Dostępne tylko dla zalogowanych)
func allocateHandler(c *gin.Context) {
	// Pobieramy ID użytkownika z kontekstu (ustawione przez Middleware)
	userID, _ := c.Get("userID") // "rzutowanie" na int

	// 1. Sprawdzenie limitu tuneli
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tunnels WHERE user_id = ?", userID).Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Błąd bazy"})
		return
	}

	if count >= MaxTunnels {
		c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Osiągnięto limit tuneli (%d)", MaxTunnels)})
		return
	}

	// 2. Szukanie wolnego portu
	allocatedPort, err := findAndReservePort(userID.(int))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Brak wolnych portów!"})
		return
	}

	// 3. Odpowiedź
	subdomain := fmt.Sprintf("srv-%d", allocatedPort)
	resp := AllocationResponse{
		ServerAddress: Domain,
		PublicPort:    allocatedPort,
		Subdomain:     subdomain,
		FullAddress:   fmt.Sprintf("%s.%s", subdomain, Domain),
	}
	c.JSON(http.StatusOK, resp)
}

// Lista tuneli użytkownika
func myTunnelsHandler(c *gin.Context) {
	userID, _ := c.Get("userID")

	rows, err := db.Query("SELECT port FROM tunnels WHERE user_id = ?", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Błąd bazy"})
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

// --- MIDDLEWARE (OCHRONIARZ) ---

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Wymagane logowanie"})
			c.Abort()
			return
		}

		// Oczekujemy nagłówka: "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Błędny format tokenu"})
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Nieprawidłowy token"})
			c.Abort()
			return
		}

		// Zapisz dane usera w kontekście, aby kolejne funkcje mogły z nich skorzystać
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// Funkcja pomocnicza do rezerwacji w DB
func findAndReservePort(userID int) (int, error) {
	for port := MinPort; port <= MaxPort; port++ {
		_, err := db.Exec("INSERT INTO tunnels (port, user_id) VALUES (?, ?)", port, userID)
		if err == nil {
			return port, nil
		}
	}
	return 0, fmt.Errorf("brak miejsc")
}