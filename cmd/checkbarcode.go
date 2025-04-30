package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math" // Used for finding shortest length
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/aquasecurity/table"     // Use existing table library
	"github.com/fatih/color"            // For colored output
	"github.com/schollz/progressbar/v3" // For progress bar
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3" // YAML parser
)

// --- Data Structures ---
type fileToProcess struct {
	SampleName     string
	RelativePath   string
	AbsolutePath   string
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
		"Error Reading":             true, // Used as prefix check
		"No Headers/Barcodes Found": true,
	}
	// Flags
	yamlTopKey        string
	numRecordsToCheck int
)

const defaultNumRecordsToCheck = 1000
const defaultMaxWorkers = 4 // Default max concurrent workers

// --- Cobra Command Definition ---
var checkbarcodeCmd = &cobra.Command{
	Use:   "checkbarcode [yaml-file]",
	Short: "Check barcode uniformity in FASTQ files listed in YAML",
	Long: `Processes FASTQ R1 files listed in a YAML config (supports legacy and new formats),
maintaining the original order from the YAML file.
Extracts the most common barcode from the first N records (default 1000).
Compares barcodes within a sample group based on the shortest length in that group,
treating 'N' as a wildcard. Displays results in a table with automatically merged sample names,
cyclically colored R1 file names, and highlighting for non-uniform/error barcodes.
Use --key (-k) to specify the YAML top-level key and --num-records (-n) to change the number of records scanned.`,
	Args: cobra.ExactArgs(1), // Requires exactly one argument: the YAML file path
	Run: func(cmd *cobra.Command, args []string) {
		// Compile regex once
		barcodeRegex = regexp.MustCompile(`^[ACGTN+]+$`)
		// The yamlTopKey and numRecordsToCheck variables will be populated by cobra
		runCheckBarcode(args[0], yamlTopKey, numRecordsToCheck)
	},
}

// init function to add command and define flags
func init() {
	rootCmd.AddCommand(checkbarcodeCmd)
	checkbarcodeCmd.Flags().StringVarP(&yamlTopKey, "key", "k", "samples", "Top-level key in YAML file containing sample definitions")
	checkbarcodeCmd.Flags().IntVarP(&numRecordsToCheck, "num-records", "n", defaultNumRecordsToCheck, "Number of FASTQ records (x4 lines) to check per file")
}

// --- Core Logic ---
func runCheckBarcode(yamlFilePath string, topKey string, recordsToCheck int) {
	// 1. Read and Parse YAML into generic structure
	yamlDataAny, err := readYamlConfigGeneric(yamlFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading YAML: %v\n", err)
		os.Exit(1)
	}

	// 2. Gather R1 File Paths while trying to preserve original order
	filesToProcess, err := gatherFilePathsGeneric(yamlDataAny, yamlFilePath, topKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing YAML data: %v\n", err)
		os.Exit(1)
	}
	if len(filesToProcess) == 0 {
		color.Yellow("No valid R1 files found to process under key '%s' in the YAML file.", topKey)
		return
	}

	// 3. Process Files Concurrently (results potentially out of order)
	unorderedResults := processFilesConcurrentlySimple(filesToProcess, recordsToCheck)

	// 4. Prepare Data for Table
	if len(unorderedResults) > 0 {
		// Reorder results based on the original filesToProcess order
		results := reorderResults(filesToProcess, unorderedResults)

		// Perform uniformity check (order doesn't matter for this)
		barcodeGroups := groupBarcodes(results)
		isGroupUniform := checkGroupUniformityPrefix(barcodeGroups)

		// Print table using the correctly ordered results slice
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
	var data map[string]any // Use 'any' instead of 'interface{}'
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
	samplesMap, ok := samplesAny.(map[string]any) // Use 'any'
	if !ok {
		return nil, fmt.Errorf("expected a map of samples under the key '%s', but got %T", topKey, samplesAny)
	}

	// Get sample names. Note: Iteration order over a map is not guaranteed.
	// If strict YAML source order is critical, a different YAML parsing approach (e.g., using yaml.Node) is needed.
	sampleNames := make([]string, 0, len(samplesMap))
	for k := range samplesMap {
		sampleNames = append(sampleNames, k)
	}
	// Optional: Sort sample names alphabetically here if consistent-but-not-yaml order is desired.
	// sort.Strings(sampleNames)

	for _, sampleName := range sampleNames {
		sampleDataAny := samplesMap[sampleName]
		var runsList []any // Use 'any'

		// Check for legacy format or new format with "data" key
		if runsDirect, ok := sampleDataAny.([]any); ok { // Use 'any'
			runsList = runsDirect
		} else if runsIndirectMap, ok := sampleDataAny.(map[string]any); ok { // Use 'any'
			if dataVal, dataKeyExists := runsIndirectMap["data"]; dataKeyExists {
				if runsDataList, ok := dataVal.([]any); ok { // Use 'any'
					runsList = runsDataList
				} else {
					color.Yellow("Warning: Sample '%s' has 'data' key but value not list (%T), skipping.", sampleName, dataVal)
					continue
				}
			} else {
				color.Yellow("Warning: Sample '%s' has map structure but no 'data' key, skipping.", sampleName)
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

		// Process the extracted runsList
		for i, runAny := range runsList {
			runMap, ok := runAny.(map[string]any) // Use 'any'
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
				color.Yellow("Warning: Sample '%s', run %d has invalid/empty 'R1' path (%T), skipping.", sampleName, i+1, r1Any)
				continue
			}

			// Expand user home dir if path starts with ~
			if strings.HasPrefix(r1RelativePath, "~") {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					color.Yellow("Warning: Cannot get home dir for path '%s', sample '%s'. Skipping.", r1RelativePath, sampleName)
					continue
				}
				r1RelativePath = filepath.Join(homeDir, r1RelativePath[1:])
			}

			// Construct absolute path
			var r1AbsPath string
			if filepath.IsAbs(r1RelativePath) {
				r1AbsPath = r1RelativePath
			} else {
				r1AbsPath = filepath.Join(yamlDir, r1RelativePath)
			}
			r1AbsPath = filepath.Clean(r1AbsPath)

			// Add file details to the list to be processed
			filesToProcess = append(filesToProcess, fileToProcess{
				SampleName:     sampleName,
				RelativePath:   r1RelativePath, // Store relative path for display/keying
				AbsolutePath:   r1AbsPath,
				RecordsToCheck: defaultNumRecordsToCheck, // Will be updated later if flag used
			})
		} // End loop through runsList
	} // End loop through samplesMap
	return filesToProcess, nil
}

// --- Simplified Concurrent File Processing ---
func processFilesConcurrentlySimple(files []fileToProcess, recordsToCheck int) []processResult {
	// Slice to collect potentially unordered results
	unorderedResults := make([]processResult, 0, len(files))
	resultChannel := make(chan processResult, len(files))
	var wg sync.WaitGroup

	bar := progressbar.NewOptions(len(files),
		progressbar.OptionSetDescription("[cyan]Processing R1 files..."),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowCount(),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{Saucer: "[green]=[reset]", SaucerHead: "[green]>[reset]", SaucerPadding: " ", BarStart: "[", BarEnd: "]"}),
	)

	numWorkers := min(defaultMaxWorkers, len(files)) // Use min (Go 1.21+)
	jobs := make(chan fileToProcess, len(files))     // Channel for jobs

	// Start workers
	wg.Add(numWorkers)
	for w := range numWorkers { // Use range (Go 1.22+)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				// Ensure the correct recordsToCheck value is used from the job struct
				barcode := getBarcodeFromFastqGo(job.AbsolutePath, job.RecordsToCheck)
				resultChannel <- processResult{
					SampleName:   job.SampleName,
					RelativePath: job.RelativePath,
					Barcode:      barcode,
				}
			}
		}(w)
	}

	// Send jobs
	for i := range files {
		files[i].RecordsToCheck = recordsToCheck // Set the value from the flag
		jobs <- files[i]
	}
	close(jobs) // All jobs sent

	// Wait for workers in a separate goroutine to allow concurrent collection
	go func() {
		wg.Wait()
		close(resultChannel) // Close results channel *after* all workers finish
	}()

	// Collect results as they arrive
	for result := range resultChannel {
		unorderedResults = append(unorderedResults, result)
		_ = bar.Add(1)
	}

	_ = bar.Finish()
	fmt.Fprintln(os.Stderr) // Newline after progress bar

	return unorderedResults
}

// --- Reordering Function ---
func reorderResults(originalOrder []fileToProcess, unorderedResults []processResult) []processResult {
	orderedResults := make([]processResult, len(originalOrder))
	resultsMap := make(map[string]processResult, len(unorderedResults))
	for _, res := range unorderedResults {
		// Use RelativePath as the key, assuming it uniquely identifies the run within the YAML context
		resultsMap[res.RelativePath] = res
	}

	for i, fileInfo := range originalOrder {
		if res, ok := resultsMap[fileInfo.RelativePath]; ok {
			orderedResults[i] = res
		} else {
			// Fallback for missing results
			orderedResults[i] = processResult{
				SampleName:   fileInfo.SampleName,
				RelativePath: fileInfo.RelativePath,
				Barcode:      "Result Missing?",
			}
			color.Red("Error: Missing result for file %s", fileInfo.RelativePath)
		}
	}
	return orderedResults
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

func areBarcodesCompatibleGo(bc1, bc2 string, minLength int) bool {
	if len(bc1) < minLength || len(bc2) < minLength {
		return false
	}
	for i := 0; i < minLength; i++ {
		char1 := bc1[i]
		char2 := bc2[i]
		if char1 != 'N' && char2 != 'N' && char1 != char2 {
			return false
		}
	}
	return true
}

// --- Grouping and Uniformity Check ---
func groupBarcodes(results []processResult) map[string][]string {
	groups := make(map[string][]string) // Use map for grouping by sample name
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

func checkGroupUniformityPrefix(barcodeGroups map[string][]string) map[string]bool {
	isUniform := make(map[string]bool)
	for sample, barcodes := range barcodeGroups {
		if len(barcodes) <= 1 {
			isUniform[sample] = true
			continue
		}
		shortestLen := math.MaxInt32
		for _, bc := range barcodes {
			if len(bc) < shortestLen {
				shortestLen = len(bc)
			}
		}
		if shortestLen == math.MaxInt32 || shortestLen == 0 {
			isUniform[sample] = true
			continue
		}
		referenceBarcode := barcodes[0]
		allCompatible := true
		for i := 1; i < len(barcodes); i++ {
			if !areBarcodesCompatibleGo(referenceBarcode, barcodes[i], shortestLen) {
				allCompatible = false
				break
			}
		}
		isUniform[sample] = allCompatible
	}
	return isUniform
}

// --- Table Generation (Using SetAutoMerge, original order, re-enabled colors) ---
func printResultsTableAqua(results []processResult, isGroupUniform map[string]bool, yamlBaseName string, recordsChecked int) {
	t := table.New(os.Stdout)
	t.SetAutoMerge(true) // Enable AutoMerge

	// Define colors
	colorCycle := []*color.Color{color.New(color.FgMagenta), color.New(color.FgCyan)}
	redColor := color.New(color.FgRed, color.Bold)
	yellowColor := color.New(color.FgYellow)
	greenColor := color.New(color.FgGreen)

	// Create colored headers
	header1 := color.New(color.FgCyan, color.Bold).Sprint("Sample")
	header2 := color.New(color.FgCyan, color.Bold).Sprint("R1 File")
	header3 := color.New(color.FgCyan, color.Bold).Sprintf("Most Common Barcode\n(first %d records)", recordsChecked)

	// Set table properties
	t.SetHeaders(header1, header2, header3)
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	// Variables for row processing logic
	previousSampleNameForColor := ""
	currentColorIndex := -1

	// Iterate through results IN THE PRESERVED ORIGINAL ORDER
	for _, row := range results {
		currentSampleName := row.SampleName
		displayR1 := row.RelativePath
		displayBarcode := row.Barcode

		// --- Styling Logic ---
		var activeColor *color.Color

		// 1. R1 Color Cycling
		if currentSampleName != previousSampleNameForColor {
			currentColorIndex = (currentColorIndex + 1) % len(colorCycle)
		}
		activeColor = colorCycle[currentColorIndex]
		styledR1 := activeColor.Sprint(displayR1)

		// 2. Barcode Highlighting
		styledBarcode := ""
		isUniform := isGroupUniform[currentSampleName] // Lookup uniformity for the group
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
			styledBarcode = redColor.Sprint(displayBarcode) // Style red if group not uniform
		} else {
			styledBarcode = greenColor.Sprint(displayBarcode)
		} // Style green if uniform

		// --- Add Row Data ---
		// Pass PLAIN sample name for AutoMerge logic to work correctly.
		t.AddRow(currentSampleName, styledR1, styledBarcode)

		// Update tracker for the next iteration's color cycling check
		previousSampleNameForColor = currentSampleName
	}

	fmt.Println()                               // Newline before table
	t.Render()                                  // Print the table
	fmt.Println("Processed on " + yamlBaseName) // Print caption separately
}
