package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

/* ---------- Custom Help ---------- */

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `
rncs  —  RNC lookup in DGII CSV

USAGE (CLI mode):
  %[1]s <RNC>

Example:
  %[1]s 132138279

USAGE (API mode):
  sudo %[1]s --foreground [port]

  If [port] is not specified, 9922 is used.
  Exposed endpoints: GET  /api/checkrnc/{RNC}
                    POST /api/reload           (hot reload CSV)

Flags:
`, os.Args[0])
	flag.PrintDefaults()
	fmt.Println()
}

/* ---------- Tipos ---------- */

type empresaRaw struct {
	RNC             string
	RazonSocial     string
	NombreComercial string
	Estado          string
}

type empresaAPI struct {
	RNC           string `json:"rnc"`
	SocialName    string `json:"socialName"`
	ComercialName string `json:"comercialName"`
	Status        string `json:"status"`
}

type apiErr struct {
	Error string `json:"error"`
}

/* ---------- Flags ---------- */

var (
	foreground bool
)

const csvFileName = "rncs.csv"

func init() {
	flag.BoolVar(&foreground, "foreground", false, "Run in API (HTTP) mode")
	flag.Usage = usage
	flag.Parse()
}

/* ---------- Índice en memoria ---------- */

var (
	once     sync.Once
	idxMutex sync.RWMutex
	rncIndex map[string]empresaAPI
	idxErr   error
)

func ensureIndex() error {
	once.Do(func() {
		rncIndex, idxErr = buildIndex(csvFileName)
	})
	return idxErr
}

func reloadIndex() error {
	idxMutex.Lock()
	defer idxMutex.Unlock()
	m, err := buildIndex(csvFileName)
	if err != nil {
		return err
	}
	rncIndex = m
	return nil
}

func buildIndex(path string) (map[string]empresaAPI, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := readAllCSV(f)
	if err != nil {
		return nil, err
	}

	idx := make(map[string]empresaAPI, len(rows))
	for i, row := range rows {
		if i == 0 || len(row) < 5 {
			continue
		}
		raw := empresaRaw{
			RNC:             strings.TrimSpace(row[0]),
			RazonSocial:     strings.TrimSpace(row[1]),
			NombreComercial: strings.TrimSpace(row[2]),
			Estado:          strings.TrimSpace(row[4]),
		}
		idx[raw.RNC] = mapToAPI(raw)
	}
	log.Printf("Index loaded: %d entries", len(idx))
	return idx, nil
}

func mapToAPI(e empresaRaw) empresaAPI {
	return empresaAPI{
		RNC:           e.RNC,
		SocialName:    e.RazonSocial,
		ComercialName: e.RazonSocial, // mismo valor
		Status:        e.Estado,
	}
}

/* ---------- Búsqueda ---------- */

func consultarRNC(rnc string) (empresaAPI, error) {
	if err := ensureIndex(); err != nil {
		return empresaAPI{}, err
	}
	idxMutex.RLock()
	defer idxMutex.RUnlock()
	if emp, ok := rncIndex[rnc]; ok {
		return emp, nil
	}
	return empresaAPI{}, errors.New("not found")
}

/* ---------- main ---------- */

func main() {
	if len(os.Args) == 1 { // no arguments -> help
		usage()
		return
	}

	if err := ensureCSVExists(csvFileName); err != nil {
		log.Fatalf("Could not obtain the CSV file: %v", err)
	}

	if foreground {
		// Build index before accepting requests
		if err := ensureIndex(); err != nil {
			log.Fatalf("Could not load CSV: %v", err)
		}
		startHTTP()
	} else {
		runCLI()
	}
}

/* ---------- CLI ---------- */

func runCLI() {
	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Error: missing RNC")
		usage()
		os.Exit(1)
	}
	rnc := args[0]

	out, err := consultarRNC(rnc)
	if err != nil {
		j, _ := json.MarshalIndent(apiErr{Error: "This RNC does not exist"}, "", "  ")
		fmt.Println(string(j))
		os.Exit(1)
	}
	j, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(j))
}

/* ---------- HTTP + CORS Middleware ---------- */

func startHTTP() {
	const defaultPort = 9922
	port := defaultPort

	// Parse optional port arg
	args := flag.Args()
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "Error: too many arguments in API mode")
		usage()
		os.Exit(1)
	}
	if len(args) == 1 {
		p, err := strconv.Atoi(args[0])
		if err != nil || p <= 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "Error: invalid port \"%s\"\n", args[0])
			usage()
			os.Exit(1)
		}
		port = p
	}

	// Tu multiplexor original
	mux := http.NewServeMux()

	// Rutas existentes...
	mux.HandleFunc("/api/checkrnc/", logRequest(func(w http.ResponseWriter, r *http.Request) {
		rnc := strings.TrimPrefix(r.URL.Path, "/api/checkrnc/")
		if rnc == "" {
			writeErr(w, http.StatusBadRequest, "RNC not provided")
			return
		}
		out, err := consultarRNC(rnc)
		if err != nil {
			writeErr(w, http.StatusNotFound, "This RNC does not exist")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/checkcedula/{CEDULA}
	mux.HandleFunc("/api/checkcedula/", logRequest(func(w http.ResponseWriter, r *http.Request) {
		cedula := strings.TrimPrefix(r.URL.Path, "/api/checkcedula/")
		if cedula == "" {
			writeErr(w, http.StatusBadRequest, "Cedula not provided")
			return
		}
		url := fmt.Sprintf("https://api.digital.gob.do/v3/cedulas/%s/validate", cedula)
		resp, err := http.Get(url)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "Error contacting external API")
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	mux.HandleFunc("/api/reload", logRequest(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if _, err := os.Stat(csvFileName); err == nil {
			_ = os.Remove(csvFileName)
		}
		if err := descargarCSV(csvFileName); err != nil {
			writeErr(w, http.StatusInternalServerError, "Error downloading CSV: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	}))

	// Logging middleware
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{ResponseWriter: w, status: 0, body: &strings.Builder{}}
		mux.ServeHTTP(rec, r)
		ip := r.RemoteAddr
		if ipHeader := r.Header.Get("X-Forwarded-For"); ipHeader != "" {
			ip = ipHeader
		}
		log.Printf("[API] %s %s %d %s\nOutput: %s", ip, r.URL.Path, rec.status, r.Method, rec.body.String())
	})

	// === CORS handler ===
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Permitir cualquier origen
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Métodos permitidos
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		// Headers permitidos
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			// Responder preflight
			w.WriteHeader(http.StatusOK)
			return
		}
		// Pasar al siguiente
		loggedMux.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      corsHandler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("HTTP server with CORS at %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiErr{Error: msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

/* ---------- CSV helper ---------- */
func readAllCSV(f *os.File) ([][]string, error) {
	r := csv.NewReader(f)
	r.LazyQuotes = true
	if rec, err := r.ReadAll(); err == nil {
		return rec, nil
	}

	// Retry as Windows-1252
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	dec := transform.NewReader(f, charmap.Windows1252.NewDecoder())
	r = csv.NewReader(dec)
	r.LazyQuotes = true
	return r.ReadAll()
}

/* ---------- CSV existence ---------- */

var (
	httpClient = &http.Client{Timeout: 60 * time.Second}
	csvOnce    sync.Once
	csvErr     error
)

func ensureCSVExists(path string) error {
	csvOnce.Do(func() {
		csvErr = descargarCSV(path)
	})
	return csvErr
}

func descargarCSV(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}
	log.Printf("CSV file not found, downloading from DGII...")

	tmpDir := "tmp_rncs"
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("error creating temporary folder: %w", err)
	}
	tmpZipPath := filepath.Join(tmpDir, "RNC_CONTRIBUYENTES.zip")

	// Download ZIP with User-Agent
	req, err := http.NewRequest("GET", "https://dgii.gov.do/app/WebApps/Consultas/RNC/RNC_CONTRIBUYENTES.zip", nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error downloading ZIP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error downloading ZIP: %s", resp.Status)
	}

	outZip, err := os.Create(tmpZipPath)
	if err != nil {
		return fmt.Errorf("error creating temporary ZIP file: %w", err)
	}
	if _, err := io.Copy(outZip, resp.Body); err != nil {
		outZip.Close()
		return fmt.Errorf("error saving ZIP: %w", err)
	}
	outZip.Close()

	// Open ZIP and extract CSV
	zr, err := zip.OpenReader(tmpZipPath)
	if err != nil {
		os.Remove(tmpZipPath)
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("error opening ZIP: %w", err)
	}
	defer zr.Close()

	var found bool
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			rc, err := f.Open()
			if err != nil {
				break
			}

			out, err := os.Create(path)
			if err != nil {
				rc.Close()
				break
			}
			buf := make([]byte, 32*1024)
			if _, err := io.CopyBuffer(out, rc, buf); err != nil {
				out.Close()
				rc.Close()
				break
			}
			rc.Close()
			out.Close()
			found = true
			break
		}
	}

	// Safe cleanup
	os.Remove(tmpZipPath)
	_ = os.RemoveAll(tmpDir)

	if !found {
		return errors.New("CSV file not found in ZIP")
	}
	log.Printf("CSV file downloaded and extracted to: %s", path)

	// Automatically reload the in-memory index
	if err := reloadIndex(); err != nil {
		log.Printf("Error reloading index after CSV download: %v", err)
	}

	return nil
}

// Middleware for logging requests
func logRequest(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Capture the response
		rec := &responseRecorder{ResponseWriter: w, status: 0, body: &strings.Builder{}}
		handler(rec, r)
		ip := r.RemoteAddr
		if ipHeader := r.Header.Get("X-Forwarded-For"); ipHeader != "" {
			ip = ipHeader
		}
		log.Printf("[API] %s %s %d %s\nOutput: %s", ip, r.URL.Path, rec.status, r.Method, rec.body.String())
	}
}

// responseRecorder para capturar la salida
type responseRecorder struct {
	http.ResponseWriter
	status int
	body   *strings.Builder
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
