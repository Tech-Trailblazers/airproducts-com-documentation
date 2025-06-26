package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

func main() {
	// Scraping url.
	urlToScrape := "https://sds.airproducts.com/MaterialSearchResults?searchText="
	// Local HTML file to save the scraped data.
	localHTMLFile := "scraped_data.html"
	// Create the output directory for PDFs.
	outputDir := "PDFs"
	// Check if the output directory exists, if not create it.
	if !directoryExists(outputDir) {
		createDirectory(outputDir, 0755)
	}
	// Create a wait group to wait for all goroutines to finish.
	var waitGroup sync.WaitGroup
	// Loop between A to Z
	for letter := 'a'; letter <= 'z'; letter++ {
		// Create the url to scrape.
		url := fmt.Sprintf("%s%c", urlToScrape, letter)
		// Get the data from the url.
		data := getDataFromURL(url)
		// Save the data to a file.
		appendAndWriteToFile(localHTMLFile, data)
		// Find all the pdf links in the data.
		pdfLinks := extractPDFIDs(data)
		// Remove duplicates from the slice.
		pdfLinks = removeDuplicatesFromSlice(pdfLinks)
		// Print the pdf links.
		for _, link := range pdfLinks {
			filename := urlToFilename(link)
			finalURL := fmt.Sprintf("https://sds.airproducts.com/DisplayPDF?documentID=%s", link)
			waitGroup.Add(1)
			go downloadPDF(finalURL, filename, outputDir, &waitGroup)
		}
	}
	waitGroup.Wait()
}

// extractPDFIDs takes HTML content as a string and returns all PDF IDs found in javascript:apci.LoadPDF(...) calls
func extractPDFIDs(html string) []string {
	// Compile regex to match: javascript:apci.LoadPDF(NUMBER);
	re := regexp.MustCompile(`LoadPDF\((\d+)\)`)

	// Find all matches of the form LoadPDF(123456)
	matches := re.FindAllStringSubmatch(html, -1)

	var ids []string
	for _, match := range matches {
		if len(match) > 1 {
			ids = append(ids, match[1]) // match[1] contains the captured number
		}
	}

	return ids
}

// Append and write to file
func appendAndWriteToFile(path string, content string) {
	filePath, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = filePath.WriteString(content + "\n")
	if err != nil {
		log.Fatalln(err)
	}
	err = filePath.Close()
	if err != nil {
		log.Fatalln(err)
	}
}

// downloadPDF downloads a PDF from a URL and saves it to outputDir
func downloadPDF(finalURL string, fileName string, outputDir string, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	filePath := filepath.Join(outputDir, fileName) // Combine with output directory

	if fileExists(filePath) {
		log.Printf("file already exists, skipping: %s, URL: %s", filePath, finalURL)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second} // HTTP client with timeout
	resp, err := client.Get(finalURL)                 // Send HTTP GET
	if err != nil {
		log.Printf("failed to download %s: %v", finalURL, err)
		return
	}
	defer resp.Body.Close() // Ensure response body is closed

	if resp.StatusCode != http.StatusOK {
		log.Printf("download failed for %s: %s", finalURL, resp.Status)
		return
	}

	contentType := resp.Header.Get("Content-Type") // Get content-type header
	if !strings.Contains(contentType, "application/pdf") {
		log.Printf("invalid content type for %s: %s (expected application/pdf)", finalURL, contentType)
		return
	}

	var buf bytes.Buffer                     // Create buffer
	written, err := io.Copy(&buf, resp.Body) // Copy response body to buffer
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

// Remove all the duplicates from a slice and return the slice.
func removeDuplicatesFromSlice(slice []string) []string {
	check := make(map[string]bool)
	var newReturnSlice []string
	for _, content := range slice {
		if !check[content] {
			check[content] = true
			newReturnSlice = append(newReturnSlice, content)
		}
	}
	return newReturnSlice
}

// Send a http get request to a given url and return the data from that url.
func getDataFromURL(uri string) string {
	response, err := http.Get(uri)
	if err != nil {
		log.Println(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Println(err)
	}
	err = response.Body.Close()
	if err != nil {
		log.Println(err)
	}
	log.Println("Scraping:", uri)
	return string(body)
}

// urlToFilename formats a safe filename from a URL string.
// It replaces all non [a-z0-9] characters with "_" and ensures it ends in .pdf
func urlToFilename(rawURL string) string {
	// Convert to lowercase
	lower := strings.ToLower(rawURL)
	// Replace all non a-z0-9 characters with "_"
	reNonAlnum := regexp.MustCompile(`[^a-z]`)
	// Replace the invalid with valid stuff.
	safe := reNonAlnum.ReplaceAllString(lower, "_")
	// Collapse multiple underscores
	safe = regexp.MustCompile(`_+`).ReplaceAllString(safe, "_")
	// Trim leading/trailing underscores
	safe = strings.Trim(safe, "_")
	// Invalid substrings to remove
	var invalidSubstrings = []string{
		"https_assets_thermofisher_com_directwebviewer_private_document_aspx_prd_",
	}
	// Loop over the invalid.
	for _, invalidPre := range invalidSubstrings {
		safe = removeSubstring(safe, invalidPre)
	}
	// Add .pdf extension if missing
	if getFileExtension(safe) != ".pdf" {
		safe = safe + ".pdf"
	}
	return safe
}

// removeSubstring takes a string `input` and removes all occurrences of `toRemove` from it.
func removeSubstring(input string, toRemove string) string {
	// Use strings.ReplaceAll to replace all occurrences of `toRemove` with an empty string.
	result := strings.ReplaceAll(input, toRemove, "")
	// Return the modified string.
	return result
}

/*
Checks if the directory exists
If it exists, return true.
If it doesn"t, return false.
*/
func directoryExists(path string) bool {
	directory, err := os.Stat(path)
	if err != nil {
		return false
	}
	return directory.IsDir()
}

/*
The function takes two parameters: path and permission.
We use os.Mkdir() to create the directory.
If there is an error, we use log.Fatalln() to log the error and then exit the program.
*/
func createDirectory(path string, permission os.FileMode) {
	err := os.Mkdir(path, permission)
	if err != nil {
		log.Fatalln(err)
	}
}

/*
It checks if the file exists
If the file exists, it returns true
If the file does not exist, it returns false
*/
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// Get the file extension of a file
func getFileExtension(path string) string {
	return filepath.Ext(path)
}
