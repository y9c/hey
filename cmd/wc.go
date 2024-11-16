package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	lineFlag bool // -l flag for line count
	wordFlag bool // -w flag for word count
	charFlag bool // -c flag for character count

	wcCmd = &cobra.Command{
		Use:   "wc [files...]",
		Short: "Count lines, words, and characters in files (gzip supported)",
		Long: `A custom implementation of wc that supports gzip-compressed files,
optimized line counting for uncompressed files, and optional word and character counting.
Directories are automatically ignored.`,
		Args: cobra.MinimumNArgs(1), // Requires at least one file as an argument
		Run: func(cmd *cobra.Command, args []string) {
			for _, filePath := range args {
				processFile(filePath)
			}
		},
	}
)

func init() {
	rootCmd.AddCommand(wcCmd)
	wcCmd.Flags().BoolVarP(&lineFlag, "lines", "l", false, "Count the number of lines")
	wcCmd.Flags().BoolVarP(&wordFlag, "words", "w", false, "Count the number of words")
	wcCmd.Flags().BoolVarP(&charFlag, "chars", "c", false, "Count the number of characters")
}

func processFile(filePath string) {
	// Check if the path is a directory
	info, err := os.Stat(filePath)
	if err != nil {
		fmt.Printf("Error accessing file %s: %v\n", filePath, err)
		return
	}
	if info.IsDir() {
		// Skip directories
		fmt.Printf("Skipping directory: %s\n", filePath)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	var reader io.Reader
	isGzip := strings.HasSuffix(file.Name(), ".gz")
	if isGzip {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			fmt.Printf("Error reading gzip file %s: %v\n", filePath, err)
			return
		}
		defer gzReader.Close()
		reader = gzReader
	} else {
		reader = file
	}

	lineCount, wordCount, charCount := 0, 0, 0

	// Use appropriate method for line counting
	if lineFlag || (!lineFlag && !wordFlag && !charFlag) {
		if isGzip {
			lineCount = countLinesWithScanner(reader)
		} else {
			lineCount = quickCountLines(reader)
		}
	}

	// Count words and characters if corresponding flags are set
	if wordFlag || charFlag {
		reader = resetReader(filePath)
		if reader == nil {
			return // Skip further processing if reader reset fails
		}
		wordCount, charCount = countWordsAndChars(reader)
	}

	// Output results
	fmt.Printf("%s\t", filePath)
	if lineFlag || (!lineFlag && !wordFlag && !charFlag) {
		fmt.Printf("Lines: %d\t", lineCount)
	}
	if wordFlag {
		fmt.Printf("Words: %d\t", wordCount)
	}
	if charFlag {
		fmt.Printf("Chars: %d\t", charCount)
	}
	fmt.Println()
}

func quickCountLines(reader io.Reader) int {
	const bufferSize = 16 * 1024
	buffer := make([]byte, bufferSize)

	totalLines := 0
	var wg sync.WaitGroup
	lineCountCh := make(chan int, runtime.NumCPU())

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localCount := 0
			for {
				n, err := reader.Read(buffer)
				if n > 0 {
					localCount += countLinesInBuffer(buffer[:n])
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					fmt.Printf("Error reading file: %v\n", err)
					return
				}
			}
			lineCountCh <- localCount
		}()
	}

	go func() {
		wg.Wait()
		close(lineCountCh)
	}()

	for count := range lineCountCh {
		totalLines += count
	}

	return totalLines
}

func countLinesWithScanner(reader io.Reader) int {
	scanner := bufio.NewScanner(reader)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("Error scanning file: %v\n", err)
	}
	return lineCount
}

func countLinesInBuffer(buffer []byte) int {
	count := 0
	for _, b := range buffer {
		if b == '\n' {
			count++
		}
	}
	return count
}

func countWordsAndChars(reader io.Reader) (int, int) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(bufio.ScanWords)

	wordCount, charCount := 0, 0
	for scanner.Scan() {
		word := scanner.Text()
		wordCount++
		charCount += len(word)
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("Error scanning file: %v\n", err)
	}
	return wordCount, charCount
}

func resetReader(filePath string) io.Reader {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error reopening file %s: %v\n", filePath, err)
		return nil
	}
	if strings.HasSuffix(filePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			fmt.Printf("Error reading gzip file %s: %v\n", filePath, err)
			return nil
		}
		return gzReader
	}
	return file
}
