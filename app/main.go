package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type Note struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var db *sql.DB

func initDB() {
	dbHost := getEnv("DB_HOST", "postgres")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "notes_user")
	dbPassword := getEnv("DB_PASSWORD", "notes_pass")
	dbName := getEnv("DB_NAME", "notes_db")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName,
	)

	log.Printf("Connecting to database: host=%s, db=%s", dbHost, dbName)
	log.Printf("Conn str=%s", connStr)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to open database connection: %v", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Failed to ping to database: %v", err)
	}

	log.Println("Successfully connected to database")
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func main() {
	if os.Getenv("APP_ENV") != "production" {
		err := godotenv.Load()
		if err != nil {
			log.Println("No .env file found, using environment variables")
		}
	}

	maxRetries := 5
	for i := range maxRetries {
		initDB()
		if db != nil {
			break
		}
		if i < maxRetries-1 {
			log.Printf("Retrying database connection (%d/%d)...", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}

	defer db.Close()

	port := getEnv("PORT", "8080")

	http.HandleFunc("/notes", notesHandler)
	http.HandleFunc("/notes/", noteHandler)
	http.HandleFunc("/health", healthHandler)

	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	log.Println("Health check: checking database connection")
	if err := db.PingContext(ctx); err != nil {
		log.Printf("Health check FAILED: database unavailable: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Database unavailable"))
		return
	}

	log.Println("Health check: database connection OK")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func notesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "/json")

	switch r.Method {
	case "GET":
		getNotes(w, r)
	case "POST":
		addNote(w, r)
	default:
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func noteHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	idStr := r.URL.Path[len("/notes/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error": "Invalid note ID"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getNote(w, r, id)
	case "PUT":
		updateNote(w, r, id)
	case "DELETE":
		deleteNote(w, r, id)
	default:
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func addNote(w http.ResponseWriter, r *http.Request) {
	var note Note
	if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
		log.Printf("Failed to decode JSON for new note: %v", err)
		http.Error(w, `{"error": "Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if note.Title == "" {
		log.Printf("Attempt to create note with empty title")
		http.Error(w, `{"error": "Title is required"}`, http.StatusBadRequest)
		return
	}

	log.Printf("Attempting to create new note with title: '%s'", note.Title)

	query := `INSERT INTO notes (title, content) VALUES ($1, $2) RETURNING id, created_at, updated_at`
	err := db.QueryRow(query, note.Title, note.Content).Scan(&note.ID, &note.CreatedAt, &note.UpdatedAt)
	if err != nil {
		log.Printf("Database error while creating note: %v", err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully created note ID=%d with title: '%s'", note.ID, note.Title)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(note)
}

func getNotes(w http.ResponseWriter, r *http.Request) {
	log.Println("Attempting to fetch all notes")

	rows, err := db.Query("SELECT id, title, content, created_at, updated_at FROM notes ORDER BY created_at DESC")
	if err != nil {
		log.Printf("Database error while fetching notes: %v", err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var notes []Note
	noteCount := 0
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt); err != nil {
			log.Printf("Row scan error for note: %v", err)
			continue
		}
		notes = append(notes, note)
		noteCount++
	}

	log.Printf("Successfully fetched %d notes", noteCount)
	json.NewEncoder(w).Encode(notes)
}

func getNote(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("Attempting to fetch note ID=%d", id)

	var note Note
	query := "SELECT id, title, content, created_at, updated_at FROM notes WHERE id = $1"
	err := db.QueryRow(query, id).Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.UpdatedAt)

	if err == sql.ErrNoRows {
		log.Printf("Note ID=%d not found", id)
		http.Error(w, `{"error": "Note not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Database error while fetching note ID=%d: %v", id, err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully fetched note ID=%d with title: '%s'", note.ID, note.Title)
	json.NewEncoder(w).Encode(note)
}

func updateNote(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("Attempting to update note ID=%d", id)

	var note Note
	if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
		log.Printf("Failed to decode JSON for update note ID=%d: %v", id, err)
		http.Error(w, `{"error": "Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	log.Printf("Updating note ID=%d, new title: '%s'", id, note.Title)

	query := `UPDATE notes SET title = $1, content = $2, updated_at = CURRENT_TIMESTAMP 
			  WHERE id = $3 RETURNING updated_at`
	err := db.QueryRow(query, note.Title, note.Content, id).Scan(&note.UpdatedAt)

	if err == sql.ErrNoRows {
		log.Printf("Note ID=%d not found for update", id)
		http.Error(w, `{"error": "Note not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Database error while updating note ID=%d: %v", id, err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	note.ID = id
	log.Printf("Successfully updated note ID=%d", id)
	json.NewEncoder(w).Encode(note)
}

func deleteNote(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("Attempting to delete note ID=%d", id)

	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM notes WHERE id = $1)", id).Scan(&exists)
	if err != nil {
		log.Printf("Database error while checking existence of note ID=%d: %v", id, err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	if !exists {
		log.Printf("Note ID=%d not found for deletion", id)
		http.Error(w, `{"error": "Note not found"}`, http.StatusNotFound)
		return
	}

	result, err := db.Exec("DELETE FROM notes WHERE id = $1", id)
	if err != nil {
		log.Printf("Database error while deleting note ID=%d: %v", id, err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("Successfully deleted note ID=%d (rows affected: %d)", id, rowsAffected)

	w.WriteHeader(http.StatusNoContent)
}
