package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
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
Displays results in a table with visual grouping and highlighting.
Use --key (-k) to specify the YAML top-level key and --num-records (-n) to change the number of records scanned.`,
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
	// 1. Read and Parse YAML
	yamlDataAny, err := readYamlConfigGeneric(yamlFilePath) // Returns map[string]any
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading YAML: %v\n", err)
		os.Exit(1)
	}

	// 2. Gather R1 File Paths
	filesToProcess, err := gatherFilePathsGeneric(yamlDataAny, yamlFilePath, topKey) // Accepts map[string]any
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing YAML data: %v\n", err)
		os.Exit(1)
	}
	if len(filesToProcess) == 0 {
		color.Yellow("No valid R1 files found to process under key '%s' in the YAML file.", topKey)
		return
	}

	// 3. Process Files Concurrently
	results := processFilesConcurrently(filesToProcess, recordsToCheck)

	// 4. Prepare Data for Table
	if len(results) > 0 {
		sort.Slice(results, func(i, j int) bool {
			return results[i].SampleName < results[j].SampleName
		})
		barcodeGroups := groupBarcodes(results)
		isGroupUniform := checkGroupUniformity(barcodeGroups)
		printResultsTableAqua(results, isGroupUniform, filepath.Base(yamlFilePath), recordsToCheck)
	} else {
		color.Yellow("No results to display.")
	}
}

// --- YAML Parsing and File Path Gathering (Using 'any') ---

// readYamlConfigGeneric reads YAML into a generic structure using 'any' (Go 1.18+)
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

// gatherFilePathsGeneric processes the generic map using 'any' (Go 1.18+)
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

	// Iterate through samples
	for sampleName, sampleDataAny := range samplesMap {
		var runsList []any // Use 'any'

		// Check for legacy format: list directly under sample name
		if runsDirect, ok := sampleDataAny.([]any); ok { // Use 'any'
			runsList = runsDirect
		} else if runsIndirectMap, ok := sampleDataAny.(map[string]any); ok { // Use 'any'
			// Check for new format: list under "data" key
			if dataVal, dataKeyExists := runsIndirectMap["data"]; dataKeyExists {
				if runsDataList, ok := dataVal.([]any); ok { // Use 'any'
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
				color.Yellow("Warning: Sample '%s', run %d has invalid or empty 'R1' path (%T), skipping.", sampleName, i+1, r1Any)
				continue
			}

			// Expand user home dir if path starts with ~
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

			filesToProcess = append(filesToProcess, fileToProcess{
				SampleName:   sampleName,
				RelativePath: r1RelativePath,
				AbsolutePath: r1AbsPath,
				// ResultChannel, WaitGroup, RecordsToCheck added later
			})
		} // End loop through runsList
	} // End loop through samplesMap

	return filesToProcess, nil
}

// --- Concurrent File Processing (Using modern 'min' and 'range') ---

func processFilesConcurrently(files []fileToProcess, recordsToCheck int) []processResult {
	results := make([]processResult, 0, len(files))
	resultChannel := make(chan processResult, len(files))
	var wg sync.WaitGroup

	bar := progressbar.NewOptions(len(files),
		progressbar.OptionSetDescription("[cyan]Processing R1 files..."),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowCount(),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer: "[green]=[reset]", SaucerHead: "[green]>[reset]",
			SaucerPadding: " ", BarStart: "[", BarEnd: "]",
		}),
	)

	// Use min (Go 1.21+) and range (Go 1.22+)
	numWorkers := min(4, len(files)) // Default max 4 workers
	jobs := make(chan fileToProcess, len(files))

	for w := range numWorkers { // Requires Go 1.22+
		go worker(w, jobs)
	}

	wg.Add(len(files))
	for i := range files { // Use range over slice (more idiomatic than index loop)
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

// worker remains the same
func worker(id int, jobs <-chan fileToProcess) {
	for job := range jobs {
		barcode := getBarcodeFromFastqGo(job.AbsolutePath, job.RecordsToCheck)
		job.ResultChannel <- processResult{
			SampleName:   job.SampleName,
			RelativePath: job.RelativePath,
			Barcode:      barcode,
		}
		job.WaitGroup.Done()
	}
}

// --- Barcode Extraction and Compatibility (remain the same) ---
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
func areBarcodesCompatibleGo(bc1, bc2 string) bool {
	if len(bc1) != len(bc2) {
		return false
	}
	r1 := []rune(bc1)
	r2 := []rune(bc2)
	for i := range r1 {
		if r1[i] != 'N' && r2[i] != 'N' && r1[i] != r2[i] {
			return false
		}
	}
	return true
}

// --- Grouping and Uniformity Check (remain the same) ---
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
func checkGroupUniformity(barcodeGroups map[string][]string) map[string]bool {
	isUniform := make(map[string]bool)
	for sample, barcodes := range barcodeGroups {
		if len(barcodes) <= 1 {
			isUniform[sample] = true
			continue
		}
		referenceBarcode := barcodes[0]
		allCompatible := true
		for i := 1; i < len(barcodes); i++ {
			if !areBarcodesCompatibleGo(referenceBarcode, barcodes[i]) {
				allCompatible = false
				break
			}
		}
		isUniform[sample] = allCompatible
	}
	return isUniform
}

// --- Table Generation (remains the same) ---
func printResultsTableAqua(results []processResult, isGroupUniform map[string]bool, yamlBaseName string, recordsChecked int) {
	t := table.New(os.Stdout)
	colorCycle := []*color.Color{color.New(color.FgMagenta), color.New(color.FgCyan)}
	redColor := color.New(color.FgRed, color.Bold)
	yellowColor := color.New(color.FgYellow)
	greenColor := color.New(color.FgGreen)
	dimColor := color.New(color.Faint)
	header1 := color.New(color.FgCyan, color.Bold).Sprint("Sample")
	header2 := color.New(color.FgCyan, color.Bold).Sprint("R1 File")
	header3 := color.New(color.FgCyan, color.Bold).Sprintf("Most Common Barcode\n(first %d records)", recordsChecked)
	t.SetHeaders(header1, header2, header3)
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)
	previousSampleName := ""
	currentColorIndex := -1
	for _, row := range results {
		currentSampleName := row.SampleName
		displayR1 := row.RelativePath
		displayBarcode := row.Barcode
		displaySampleName := ""
		var activeColor *color.Color
		if currentSampleName != previousSampleName {
			currentColorIndex = (currentColorIndex + 1) % len(colorCycle)
			displaySampleName = currentSampleName
		} else {
			displaySampleName = dimColor.Sprint(currentSampleName)
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
		t.AddRow(displaySampleName, styledR1, styledBarcode)
		previousSampleName = currentSampleName
	}
	fmt.Println()
	t.Render()
	fmt.Println("Processed on " + yamlBaseName)
}
