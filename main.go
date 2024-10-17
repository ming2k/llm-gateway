package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type ErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

var (
	db          *sql.DB
	accessToken string
)

const (
	model    = "claude-3-5-sonnet@20240620"
	location = "us-east5"
)

func init() {
	if err := loadEnv(); err != nil {
		log.Fatalf("Failed to load .env file: %v", err)
	}
	initDB()
}

func loadEnv() error {
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("error loading .env file: %w", err)
	}
	requiredEnvs := []string{
		"APP_PORT",
		"PROJECT_ID",
		"CLIENT_EMAIL",
		"PRIVATE_KEY_ID",
		"PRIVATE_KEY",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
		"DB_PORT",
	}
	for _, env := range requiredEnvs {
		if os.Getenv(env) == "" {
			return fmt.Errorf("required environment variable not set: %s", env)
		}
	}
	return nil
}

func initDB() {
	dbURL := fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"))
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS api_keys (
			key TEXT PRIMARY KEY,
			remaining_calls INTEGER NOT NULL
		)
	`)
	if err != nil {
		log.Fatalf("Error creating table: %v", err)
	}
}

func main() {
	// Get access token
	clientEmail := os.Getenv("CLIENT_EMAIL")
	privateKeyID := os.Getenv("PRIVATE_KEY_ID")
	privateKey := os.Getenv("PRIVATE_KEY")
	newAccessToken, err := GetAccessToken(clientEmail, privateKey, privateKeyID)
	if err != nil {
		fmt.Printf("Error getting access token: %v\n", err)
		return
	}
	accessToken = newAccessToken
	// fmt.Printf("Access Token: %s\n", accessToken)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleForwardToEndpoint)
	mux.HandleFunc("/health", handleHealthCheck)

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server is running on :%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handleForwardToEndpoint(w http.ResponseWriter, r *http.Request) {
	// 只允许 POST 方法
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// TODO: setting x-api-key access time limit (with db)
	// apiKey := r.Header.Get("x-api-key")
	// if apiKey == "" {
	// 	return
	// }
	projectID := os.Getenv("PROJECT_ID")

	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:streamRawPredict",
		location, projectID, location, model)

	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Content-Type":  "application/json; charset=utf-8",
	}

	// 读取请求体
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	log.Printf("Request body: %s", string(reqBody))
	defer r.Body.Close()

	resp, err := sendRequest(url, headers, reqBody)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Request successful!")
		fmt.Println("Response:")
		fmt.Println(string(body))
	} else {
		fmt.Printf("Request failed with status code: %d\n", resp.StatusCode)
		fmt.Println("Response:")
		fmt.Println(string(body))
	}
	// TODO response in stream form
}

func sendRequest(url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: resp.StatusCode,
		Body:       io.NopCloser(bytes.NewBuffer(respBody)),
	}, nil
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "Database connection failed")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}
