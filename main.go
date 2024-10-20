package main

import (
	"bufio"
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
	// try to get the .env file from the current directory
	// if it doesn't exist, use the system's environment
	_ = godotenv.Load()

	requiredEnvs := []string{
		"APP_PORT",
		"GC_PROJECT_ID",
		"GC_CLIENT_EMAIL",
		"GC_PRIVATE_KEY_ID",
		"GC_PRIVATE_KEY",
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
	gcClientEmail := os.Getenv("GC_CLIENT_EMAIL")
	gcPrivateKeyID := os.Getenv("GC_PRIVATE_KEY_ID")
	gcPrivateKey := os.Getenv("GC_PRIVATE_KEY")
	newAccessToken, err := GetAccessToken(gcClientEmail, gcPrivateKey, gcPrivateKeyID)
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

	// 验证 API 密钥
	apiKey := r.Header.Get("x-api-key")
	if apiKey == "" {
		http.Error(w, "API key is required", http.StatusUnauthorized)
		return
	}

	// 检查并减少 API 密钥的剩余调用次数
	remainingCalls, err := checkAndDecrementAPIKey(apiKey)
	if err != nil {
		http.Error(w, "Invalid or expired API key", http.StatusUnauthorized)
		return
	}

	if remainingCalls <= 0 {
		http.Error(w, "API key has no remaining calls", http.StatusForbidden)
		return
	}

	// 设置剩余调用次数的响应头
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remainingCalls))

	// ... [其余的代码保持不变] ...

	projectID := os.Getenv("GC_PROJECT_ID")

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
		http.Error(w, fmt.Sprintf("Request failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 设置响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 创建一个缓冲读取器
	reader := bufio.NewReader(resp.Body)

	// 逐行读取响应并写入 ResponseWriter
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading from response: %v", err)
			break
		}

		// 写入每一行到 ResponseWriter
		_, err = w.Write(line)
		if err != nil {
			log.Printf("Error writing to ResponseWriter: %v", err)
			break
		}

		// 刷新写入的内容
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func checkAndDecrementAPIKey(apiKey string) (int, error) {
	var remainingCalls int
	err := db.QueryRow("SELECT remaining_calls FROM api_keys WHERE key = $1", apiKey).Scan(&remainingCalls)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("API key not found")
		}
		return 0, err
	}

	if remainingCalls <= 0 {
		return 0, nil
	}

	_, err = db.Exec("UPDATE api_keys SET remaining_calls = remaining_calls - 1 WHERE key = $1", apiKey)
	if err != nil {
		return 0, err
	}

	return remainingCalls - 1, nil
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
