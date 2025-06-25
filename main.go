package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type album struct {
	AlbumId      int    `json:"albumId"`
	Id           int    `json:"id"`
	Title        string `json:"title"`
	Url          string `json:"url"`
	ThumbnailUrl string `json:"thumbnailUrl"`
}

func main() {
	go func() {
		// This makes sure that the stream process does not take longer than 2 seconds
		// This is possible because I am streaming directly from the connection socket
		// So while I am streaming, the request connection is still open so the timeout
		// tracker is still going
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		streamToFile(ctx)
	}()

	go func() {
		fmt.Println("Serving all files in the ./html/ directory")
		http.Handle("/", http.FileServer(http.Dir("./html")))
		http.ListenAndServe(":8000", nil)
	}()

	http.HandleFunc("/events", sseHandler)
	fmt.Println("Listening on :8080")
	http.ListenAndServe(":8080", nil)
}

func sseHandler(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Set headers for CORS too
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	resp, _, err := fetchAlbums(r.Context())
	if err != nil {
		fmt.Fprintf(w, "data: %s \n\n", err.Error()) // SSE events must start with data: and end with \n\n
		flusher.Flush()
		return
	}

	body := resp.Body
	defer body.Close()

	dec := json.NewDecoder(body)
	_, err = dec.Token() // we know its an array

	for dec.More() {
		var a album
		if err := dec.Decode(&a); err != nil {
			fmt.Fprintf(w, "data: Error decoding single album %s\n\n", err.Error())
			flusher.Flush()
			return
		}

		fmt.Fprintf(w, "data: Title: %s\n\n", a.Title)
		flusher.Flush()
		time.Sleep(1 * time.Second)
	}
}

func streamToFile(ctx context.Context) {
	resp, elapsed, err := fetchAlbums(ctx)

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Error response " + err.Error())
		os.Exit(1)
	}

	file, err := os.Create("response.json")
	if err != nil {
		fmt.Println("Error creating file " + err.Error())
		os.Exit(1)
	}

	body := resp.Body
	defer body.Close() // The connection is closed only after the whole body is read

	// io.Copy(file, body) // io.Copy can handle large lifes efficiently

	manualStreamDownloadAndStreamFlush(body, file) // attempt to implement an efficient copy

	fmt.Println("Streaming duration " + elapsed.String())
}

func manualStreamDownloadAndStreamFlush(body io.Reader, file *os.File) {
	dec := json.NewDecoder(body)

	t, err := dec.Token()
	if err != nil || t != json.Delim('[') {
		fmt.Println(err.Error() + " or it is not a json array. Read token is " + fmt.Sprintf("%v", t))
		var payload []byte
		body.Read(payload)
		fmt.Println("Full body:")
		fmt.Println(string(payload))
		os.Exit(1)
	}

	enc := json.NewEncoder(file)
	file.WriteString("[")
	for dec.More() {
		var a album
		if err := dec.Decode(&a); err != nil {
			fmt.Println("Error decoding single album " + err.Error())
			os.Exit(1)
		}

		enc.Encode(a)
		file.WriteString(",")
	}

	_, err = dec.Token() // read the closing bracket (useful to validate)
	if err != nil {
		fmt.Println("Error reading closing bracket:", err)
	}

	file.WriteString("]")
}

func fetchAlbums(ctx context.Context) (*http.Response, time.Duration, error) {
	url := "https://jsonplaceholder.typicode.com/photos"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Println("Error creating req obj " + err.Error())
		os.Exit(1)
	}

	client := &http.Client{}

	now := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error making request " + err.Error())
		os.Exit(1)
	}
	return resp, time.Since(now), err
}
