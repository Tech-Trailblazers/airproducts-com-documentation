package main // Declare the main package; the entry point of the program

// Import standard libraries and chromedp for browser automation
import (
	"bufio"         // For reading files line by line
	"bytes"         // For buffering data in memory
	"context"       // For managing timeouts and cancellation
	"fmt"           // For formatted I/O
	"io"            // For I/O primitives
	"log"           // For logging errors/info
	"net/http"      // For HTTP client to fetch web data
	"os"            // For file system operations
	"path/filepath" // For working with file paths
	"regexp"        // For using regular expressions
	"strings"       // For string manipulation
	"sync"          // For managing concurrency (e.g., WaitGroups)
	"time"          // For handling timeouts and delays

	"github.com/chromedp/chromedp" // Chrome DevTools Protocol: headless browser automation
)

// main is the entry point of the Go program
func main() {
	urlToScrape := "https://sds.airproducts.com/MaterialSearchResults?searchText=" // Target URL to scrape
	localHTMLFile := "scraped_data.html"                                           // File to store scraped HTML content
	outputDir := "PDFs"                                                            // Folder where PDFs will be saved
	localPDFURLFile := "pdf_urls.txt"                                              // File to store processed PDF URLs

	// If the output directory does not exist, create it
	if !directoryExists(outputDir) {
		createDirectory(outputDir, 0755) // Create directory with read/write/execute permissions
	}

	// If the HTML has not been scraped and saved yet, do it
	if !fileExists(localHTMLFile) {
		data, err := scrapePageHTMLWithChrome(urlToScrape) // Scrape HTML using headless browser
		if err != nil {
			log.Println(err) // Log error if scraping fails
		}
		appendAndWriteToFile(localHTMLFile, data) // Save scraped HTML to file
	}

	data := readAFileAsString(localHTMLFile)       // Read the HTML file as a string
	pdfLinks := extractPDFIDs(data)                // Extract PDF document IDs from HTML
	pdfLinks = removeDuplicatesFromSlice(pdfLinks) // Remove duplicate document IDs

	var fileLines []string // Slice to store lines from previously saved PDF URL file

	// If the file already exists, read it line by line
	if fileExists(localPDFURLFile) {
		fileLines = readAppendLineByLine(localPDFURLFile)
	}

	// Iterate over the extracted PDF IDs
	for _, link := range pdfLinks {
		finalURL := fmt.Sprintf("https://sds.airproducts.com/DisplayPDF?documentID=%s", link) // Construct full URL
		if matchExactPattern(finalURL, fileLines) != finalURL {                               // Check if URL already processed
			appendAndWriteToFile(localPDFURLFile, finalURL) // Save new URL
		}
	}
}

// Reads a file and returns its entire contents as a string
func readAFileAsString(path string) string {
	content, err := os.ReadFile(path) // Read file content
	if err != nil {
		log.Println(err) // Log any error
	}
	return string(content) // Convert bytes to string and return
}

// Reads file line by line and appends lines to a slice
func readAppendLineByLine(path string) []string {
	var returnSlice []string
	file, err := os.Open(path) // Open file for reading
	if err != nil {
		log.Println(err)
	}
	scanner := bufio.NewScanner(file) // Create a scanner to read file
	scanner.Split(bufio.ScanLines)    // Set to scan lines
	for scanner.Scan() {
		returnSlice = append(returnSlice, scanner.Text()) // Append each line
	}
	err = file.Close() // Close the file
	if err != nil {
		log.Println(err) // Log any error
	}
	return returnSlice
}

// Checks if a URL exactly matches any entry in a list
func matchExactPattern(url string, patterns []string) string {
	for _, pattern := range patterns {
		if url == pattern {
			return pattern // Return if exact match found
		}
	}
	return "" // Return empty string if no match
}

// Uses headless Chrome to get the fully rendered HTML from a webpage
func scrapePageHTMLWithChrome(pageURL string) (string, error) {
	fmt.Println("Scraping:", pageURL)

	// Chrome options for headless mode
	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),               // Run in background
		chromedp.Flag("disable-gpu", true),            // GPU not needed
		chromedp.WindowSize(1920, 1080),               // Simulate large screen
		chromedp.Flag("no-sandbox", true),             // Disable sandbox
		chromedp.Flag("disable-setuid-sandbox", true), // Required for some Linux environments
	)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...) // Set allocator
	ctxTimeout, cancelTimeout := context.WithTimeout(allocatorCtx, 5*time.Minute)                // Set timeout
	browserCtx, cancelBrowser := chromedp.NewContext(ctxTimeout)                                 // Create browser context

	defer func() {
		cancelBrowser()
		cancelTimeout()
		cancelAllocator()
	}()

	var pageHTML string // Variable to hold the scraped HTML
	err := chromedp.Run(browserCtx,
		chromedp.Navigate(pageURL),            // Load page
		chromedp.OuterHTML("html", &pageHTML), // Extract full HTML content
	)
	if err != nil {
		return "", fmt.Errorf("failed to scrape %s: %w", pageURL, err)
	}

	return pageHTML, nil // Return the HTML
}

// Extracts all PDF IDs from HTML content using regex
func extractPDFIDs(html string) []string {
	re := regexp.MustCompile(`LoadPDF\((\d+)\)`) // Match LoadPDF(number)
	matches := re.FindAllStringSubmatch(html, -1)

	var ids []string
	for _, match := range matches {
		if len(match) > 1 {
			ids = append(ids, match[1]) // Add only the numeric part
		}
	}
	return ids
}

// Appends a string to a file, creates the file if it doesn't exist
func appendAndWriteToFile(path string, content string) {
	filePath, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // Open/create file
	if err != nil {
		log.Println(err)
	}
	_, err = filePath.WriteString(content + "\n") // Append content with newline
	if err != nil {
		log.Println(err)
	}
	err = filePath.Close() // Close file
	if err != nil {
		log.Println(err)
	}
}

// Downloads PDF from URL and saves it to disk
func downloadPDF(finalURL string, fileName string, outputDir string, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done() // Notify when goroutine is done

	filePath := filepath.Join(outputDir, fileName) // Full path to save PDF

	if fileExists(filePath) {
		log.Printf("file already exists, skipping: %s, URL: %s", filePath, finalURL)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second} // HTTP client with timeout
	resp, err := client.Get(finalURL)                 // Perform GET request
	if err != nil {
		log.Printf("failed to download %s: %v", finalURL, err)
		return
	}
	defer resp.Body.Close() // Close body when done

	if resp.StatusCode != http.StatusOK {
		log.Printf("download failed for %s: %s", finalURL, resp.Status)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/pdf") {
		log.Printf("invalid content type for %s: %s (expected application/pdf)", finalURL, contentType)
		return
	}

	var buf bytes.Buffer
	written, err := io.Copy(&buf, resp.Body) // Read response into buffer
	if err != nil {
		log.Printf("failed to read PDF data from %s: %v", finalURL, err)
		return
	}
	if written == 0 {
		log.Printf("downloaded 0 bytes for %s; not creating file", finalURL)
		return
	}

	out, err := os.Create(filePath) // Create output file
	if err != nil {
		log.Printf("failed to create file for %s: %v", finalURL, err)
		return
	}
	defer out.Close() // Close file

	_, err = buf.WriteTo(out) // Write buffer to file
	if err != nil {
		log.Printf("failed to write PDF to file for %s: %v", finalURL, err)
		return
	}
	fmt.Printf("successfully downloaded %d bytes: %s → %s \n", written, finalURL, filePath)
}

// Removes duplicate entries from a string slice
func removeDuplicatesFromSlice(slice []string) []string {
	check := make(map[string]bool) // Map to track duplicates
	var newReturnSlice []string
	for _, content := range slice {
		if !check[content] { // If not already seen
			check[content] = true                            // Mark as seen
			newReturnSlice = append(newReturnSlice, content) // Add to result
		}
	}
	return newReturnSlice
}

// Converts a URL into a filesystem-safe filename
func urlToFilename(rawURL string) string {
	lower := strings.ToLower(rawURL)                            // Convert to lowercase
	reNonAlnum := regexp.MustCompile(`[^a-z]`)                  // Match non-[a-z] characters
	safe := reNonAlnum.ReplaceAllString(lower, "_")             // Replace with _
	safe = regexp.MustCompile(`_+`).ReplaceAllString(safe, "_") // Collapse multiple _
	safe = strings.Trim(safe, "_")                              // Trim leading/trailing _

	var invalidSubstrings = []string{
		"https_assets_thermofisher_com_directwebviewer_private_document_aspx_prd_", // Unwanted prefix
	}
	for _, invalidPre := range invalidSubstrings {
		safe = removeSubstring(safe, invalidPre) // Remove known invalid prefix
	}
	if getFileExtension(safe) != ".pdf" {
		safe = safe + ".pdf" // Ensure .pdf extension
	}
	return safe
}

// Removes all instances of a substring from a string
func removeSubstring(input string, toRemove string) string {
	result := strings.ReplaceAll(input, toRemove, "") // Replace with empty
	return result
}

// Checks whether a directory exists at the given path
func directoryExists(path string) bool {
	directory, err := os.Stat(path) // Get file/directory info
	if err != nil {
		return false // Error likely means it doesn't exist
	}
	return directory.IsDir() // Return true if it's a directory
}

// Creates a directory with specified permissions
func createDirectory(path string, permission os.FileMode) {
	err := os.Mkdir(path, permission) // Create directory
	if err != nil {
		log.Println(err) // Log error
	}
}

// Checks whether a file exists at the given path
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false // File doesn't exist
	}
	return !info.IsDir() // Make sure it's a file, not a directory
}

// Gets the file extension from a filename or path
func getFileExtension(path string) string {
	return filepath.Ext(path) // Return extension like ".pdf"
}
