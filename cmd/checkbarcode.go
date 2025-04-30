package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math" // Import math package for MaxInt32
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/aquasecurity/table"
	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// --- Data Structures ---
type fileToProcess struct {
	SampleName     string
	RelativePath   string
	AbsolutePath   string
	ResultChannel  chan<- processResult
	WaitGroup      *sync.WaitGroup
	RecordsToCheck int
}

type processResult struct {
	SampleName   string
	RelativePath string
	Barcode      string
}

// --- Global Variables / Constants ---
var (
	barcodeRegex  *regexp.Regexp
	errorMessages = map[string]bool{
		"File Not Found":            true,
		"Not a Gzip File":           true,
		"Error Reading":             true,
		"No Headers/Barcodes Found": true,
	}
	yamlTopKey        string
	numRecordsToCheck int
)

const defaultNumRecordsToCheck = 100

// --- Cobra Command Definition ---
var checkbarcodeCmd = &cobra.Command{
	Use:   "checkbarcode [yaml-file]",
	Short: "Check barcode uniformity in FASTQ files listed in YAML",
	Long: `Processes FASTQ R1 files listed in a YAML config (supports legacy and new formats).
Extracts the most common barcode from the first N records (default 100).
Compares barcodes within a sample group based on the shortest length in that group,
treating 'N' as a wildcard. Displays results in a table with highlighting for non-uniform groups.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		barcodeRegex = regexp.MustCompile(`^[ACGTN+]+$`)
		runCheckBarcode(args[0], yamlTopKey, numRecordsToCheck)
	},
}

func init() {
	rootCmd.AddCommand(checkbarcodeCmd)
	checkbarcodeCmd.Flags().StringVarP(&yamlTopKey, "key", "k", "samples", "Top-level key in YAML file containing sample definitions")
	checkbarcodeCmd.Flags().IntVarP(&numRecordsToCheck, "num-records", "n", defaultNumRecordsToCheck, "Number of FASTQ records (x4 lines) to check per file")
}

// --- Core Logic ---
func runCheckBarcode(yamlFilePath string, topKey string, recordsToCheck int) {
	yamlDataAny, err := readYamlConfigGeneric(yamlFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading YAML: %v\n", err)
		os.Exit(1)
	}
	filesToProcess, err := gatherFilePathsGeneric(yamlDataAny, yamlFilePath, topKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing YAML data: %v\n", err)
		os.Exit(1)
	}
	if len(filesToProcess) == 0 {
		color.Yellow("No valid R1 files found to process under key '%s' in the YAML file.", topKey)
		return
	}
	results := processFilesConcurrently(filesToProcess, recordsToCheck)
	if len(results) > 0 {
		sort.Slice(results, func(i, j int) bool { return results[i].SampleName < results[j].SampleName })
		barcodeGroups := groupBarcodes(results)
		// Use the updated uniformity check
		isGroupUniform := checkGroupUniformityPrefix(barcodeGroups)
		printResultsTableAqua(results, isGroupUniform, filepath.Base(yamlFilePath), recordsToCheck)
	} else {
		color.Yellow("No results to display.")
	}
}

// --- YAML Parsing and File Path Gathering (Using 'any') ---
func readYamlConfigGeneric(yamlFilePath string) (map[string]any, error) {
	yamlFile, err := os.ReadFile(yamlFilePath)
	if err != nil {
		return nil, fmt.Errorf("reading YAML file '%s': %w", yamlFilePath, err)
	}
	var data map[string]any
	err = yaml.Unmarshal(yamlFile, &data)
	if err != nil {
		return nil, fmt.Errorf("parsing YAML file '%s': %w", yamlFilePath, err)
	}
	return data, nil
}
func gatherFilePathsGeneric(yamlDataAny map[string]any, yamlFilePath string, topKey string) ([]fileToProcess, error) {
	var filesToProcess []fileToProcess
	yamlDir := filepath.Dir(yamlFilePath)
	if topKey == "" {
		return nil, fmt.Errorf("YAML top-level key cannot be empty; provide using --key flag")
	}
	fmt.Fprintf(os.Stderr, "[dim]Using top-level key from command line: '%s'\n", topKey)
	samplesAny, ok := yamlDataAny[topKey]
	if !ok {
		return nil, fmt.Errorf("top-level key '%s' not found in YAML", topKey)
	}
	samplesMap, ok := samplesAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a map of samples under the key '%s', but got %T", topKey, samplesAny)
	}
	for sampleName, sampleDataAny := range samplesMap {
		var runsList []any
		if runsDirect, ok := sampleDataAny.([]any); ok {
			runsList = runsDirect
		} else if runsIndirectMap, ok := sampleDataAny.(map[string]any); ok {
			if dataVal, dataKeyExists := runsIndirectMap["data"]; dataKeyExists {
				if runsDataList, ok := dataVal.([]any); ok {
					runsList = runsDataList
				} else {
					color.Yellow("Warning: Sample '%s' has 'data' key but its value is not a list (%T), skipping.", sampleName, dataVal)
					continue
				}
			} else {
				color.Yellow("Warning: Sample '%s' has unexpected structure (not a list, no 'data' key), skipping.", sampleName)
				continue
			}
		} else {
			color.Yellow("Warning: Sample '%s' has unexpected value type (%T), skipping.", sampleName, sampleDataAny)
			continue
		}
		if runsList == nil {
			color.Yellow("Warning: Could not extract runs list for sample '%s', skipping.", sampleName)
			continue
		}
		for i, runAny := range runsList {
			runMap, ok := runAny.(map[string]any)
			if !ok {
				color.Yellow("Warning: Sample '%s', run %d is not a map, skipping.", sampleName, i+1)
				continue
			}
			r1Any, r1KeyExists := runMap["R1"]
			if !r1KeyExists {
				color.Yellow("Warning: Sample '%s', run %d has no 'R1' key, skipping.", sampleName, i+1)
				continue
			}
			r1RelativePath, ok := r1Any.(string)
			if !ok || r1RelativePath == "" {
				color.Yellow("Warning: Sample '%s', run %d has invalid or empty 'R1' path (%T), skipping.", sampleName, i+1, r1Any)
				continue
			}
			if strings.HasPrefix(r1RelativePath, "~") {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					color.Yellow("Warning: Could not determine home directory for path '%s' in sample '%s', skipping run.", r1RelativePath, sampleName)
					continue
				}
				r1RelativePath = filepath.Join(homeDir, r1RelativePath[1:])
			}
			var r1AbsPath string
			if filepath.IsAbs(r1RelativePath) {
				r1AbsPath = r1RelativePath
			} else {
				r1AbsPath = filepath.Join(yamlDir, r1RelativePath)
			}
			r1AbsPath = filepath.Clean(r1AbsPath)
			filesToProcess = append(filesToProcess, fileToProcess{SampleName: sampleName, RelativePath: r1RelativePath, AbsolutePath: r1AbsPath})
		}
	}
	return filesToProcess, nil
}

// --- Concurrent File Processing (Using modern 'min' and 'range') ---
func processFilesConcurrently(files []fileToProcess, recordsToCheck int) []processResult {
	results := make([]processResult, 0, len(files))
	resultChannel := make(chan processResult, len(files))
	var wg sync.WaitGroup
	bar := progressbar.NewOptions(len(files), progressbar.OptionSetDescription("[cyan]Processing R1 files..."), progressbar.OptionSetWriter(os.Stderr), progressbar.OptionShowCount(), progressbar.OptionEnableColorCodes(true), progressbar.OptionSetTheme(progressbar.Theme{Saucer: "[green]=[reset]", SaucerHead: "[green]>[reset]", SaucerPadding: " ", BarStart: "[", BarEnd: "]"}))
	numWorkers := min(4, len(files))
	jobs := make(chan fileToProcess, len(files))
	for w := range numWorkers {
		go worker(w, jobs)
	}
	wg.Add(len(files))
	for i := range files {
		files[i].ResultChannel = resultChannel
		files[i].WaitGroup = &wg
		files[i].RecordsToCheck = recordsToCheck
		jobs <- files[i]
	}
	close(jobs)
	go func() { wg.Wait(); close(resultChannel) }()
	for result := range resultChannel {
		results = append(results, result)
		_ = bar.Add(1)
	}
	_ = bar.Finish()
	fmt.Fprintln(os.Stderr)
	return results
}
func worker(id int, jobs <-chan fileToProcess) {
	for job := range jobs {
		barcode := getBarcodeFromFastqGo(job.AbsolutePath, job.RecordsToCheck)
		job.ResultChannel <- processResult{SampleName: job.SampleName, RelativePath: job.RelativePath, Barcode: barcode}
		job.WaitGroup.Done()
	}
}

// --- Barcode Extraction and Compatibility ---
func extractBarcodeFromHeaderGo(headerLine string) (string, bool) {
	parts := strings.Fields(headerLine)
	if len(parts) < 2 {
		return "", false
	}
	infoPart := parts[1]
	barcodeParts := strings.Split(infoPart, ":")
	if len(barcodeParts) == 0 {
		return "", false
	}
	potentialBarcode := barcodeParts[len(barcodeParts)-1]
	if barcodeRegex.MatchString(potentialBarcode) {
		return potentialBarcode, true
	}
	return "", false
}
func getBarcodeFromFastqGo(fastqPath string, recordsToCheck int) string {
	linesToCheck := recordsToCheck * 4
	file, err := os.Open(fastqPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "File Not Found"
		}
		return fmt.Sprintf("Error Reading (%T)", err)
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(fastqPath), ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			if err == gzip.ErrHeader || err == gzip.ErrChecksum {
				return "Not a Gzip File"
			}
			return fmt.Sprintf("Error Reading (%T)", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}
	scanner := bufio.NewScanner(reader)
	lineCounter := 0
	foundBarcodes := []string{}
	for scanner.Scan() {
		lineCounter++
		if lineCounter > linesToCheck {
			break
		}
		if lineCounter%4 == 1 {
			line := scanner.Text()
			if strings.HasPrefix(line, "@") {
				if barcode, ok := extractBarcodeFromHeaderGo(line); ok {
					foundBarcodes = append(foundBarcodes, barcode)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Sprintf("Error Reading (%T)", err)
	}
	if len(foundBarcodes) == 0 {
		return "No Headers/Barcodes Found"
	}
	counts := make(map[string]int)
	maxCount := 0
	mostCommon := ""
	for _, bc := range foundBarcodes {
		counts[bc]++
		if counts[bc] > maxCount {
			maxCount = counts[bc]
			mostCommon = bc
		}
	}
	return mostCommon
}

// Modified areBarcodesCompatibleGo to compare prefixes up to minLength
func areBarcodesCompatibleGo(bc1, bc2 string, minLength int) bool {
	// Ensure strings are long enough for the comparison length
	if len(bc1) < minLength || len(bc2) < minLength {
		// This implies an issue with how minLength was calculated or passed,
		// or very short barcodes. Treat as incompatible for safety.
		return false
	}
	for i := 0; i < minLength; i++ {
		// Use byte indexing assuming ASCII/UTF-8 compatible barcodes
		char1 := bc1[i]
		char2 := bc2[i]
		// Check for incompatibility: if neither is 'N' and they differ
		if char1 != 'N' && char2 != 'N' && char1 != char2 {
			return false
		}
	}
	// If we finish the loop without finding incompatibilities in the prefix, they are compatible
	return true
}

// --- Grouping and Uniformity Check ---
func groupBarcodes(results []processResult) map[string][]string {
	groups := make(map[string][]string)
	for _, res := range results {
		isError := false
		for msg := range errorMessages {
			if strings.HasPrefix(res.Barcode, msg) {
				isError = true
				break
			}
		}
		if !isError {
			groups[res.SampleName] = append(groups[res.SampleName], res.Barcode)
		}
	}
	return groups
}

// Updated checkGroupUniformityPrefix function
func checkGroupUniformityPrefix(barcodeGroups map[string][]string) map[string]bool {
	isUniform := make(map[string]bool)
	for sample, barcodes := range barcodeGroups {
		if len(barcodes) <= 1 {
			isUniform[sample] = true // Uniform if 0 or 1 valid barcode
			continue
		}

		// Find the minimum length among valid barcodes in this group
		shortestLen := math.MaxInt32 // Start with a large number
		for _, bc := range barcodes {
			if len(bc) < shortestLen {
				shortestLen = len(bc)
			}
		}

		// If shortestLen is still MaxInt32, it means no valid barcodes were found
		// Or if shortest length is 0, comparison is meaningless
		if shortestLen == math.MaxInt32 || shortestLen == 0 {
			isUniform[sample] = true // Treat as uniform if no comparable barcodes
			continue
		}

		// Check compatibility based on the prefix of shortestLen
		referenceBarcode := barcodes[0]
		allCompatible := true
		for i := 1; i < len(barcodes); i++ {
			// Use the compatibility check with the calculated shortest length
			if !areBarcodesCompatibleGo(referenceBarcode, barcodes[i], shortestLen) {
				allCompatible = false
				break // Found incompatibility
			}
		}
		isUniform[sample] = allCompatible
	}
	return isUniform
}

// --- Table Generation (remains the same as the last version) ---
func printResultsTableAqua(results []processResult, isGroupUniform map[string]bool, yamlBaseName string, recordsChecked int) {
	t := table.New(os.Stdout)
	t.SetAutoMerge(true)
	colorCycle := []*color.Color{color.New(color.FgMagenta), color.New(color.FgCyan)}
	redColor := color.New(color.FgRed, color.Bold)
	yellowColor := color.New(color.FgYellow)
	greenColor := color.New(color.FgGreen)
	header1 := color.New(color.FgCyan, color.Bold).Sprint("Sample")
	header2 := color.New(color.FgCyan, color.Bold).Sprint("R1 File")
	header3 := color.New(color.FgCyan, color.Bold).Sprintf("Most Common Barcode\n(first %d records)", recordsChecked)
	t.SetHeaders(header1, header2, header3)
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)
	previousSampleNameForColor := ""
	currentColorIndex := -1
	for _, row := range results {
		currentSampleName := row.SampleName
		displayR1 := row.RelativePath
		displayBarcode := row.Barcode
		var activeColor *color.Color
		if currentSampleName != previousSampleNameForColor {
			currentColorIndex = (currentColorIndex + 1) % len(colorCycle)
		}
		activeColor = colorCycle[currentColorIndex]
		styledR1 := activeColor.Sprint(displayR1)
		styledBarcode := ""
		isUniform := isGroupUniform[currentSampleName]
		isError := false
		for msg := range errorMessages {
			if strings.HasPrefix(displayBarcode, msg) {
				isError = true
				break
			}
		}
		if isError {
			styledBarcode = yellowColor.Sprint(displayBarcode)
		} else if !isUniform {
			styledBarcode = redColor.Sprint(displayBarcode)
		} else {
			styledBarcode = greenColor.Sprint(displayBarcode)
		}
		t.AddRow(currentSampleName, styledR1, styledBarcode)
		previousSampleNameForColor = currentSampleName
	}
	fmt.Println()
	t.Render()
	fmt.Println("Processed on " + yamlBaseName)
}

// --- Entry Point / Other Helpers (ensure main package calls cmd.Execute()) ---
// ... (make sure functions like areBarcodesCompatibleGo are defined correctly) ...
