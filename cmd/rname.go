package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/aquasecurity/table" // Added for table output
	"github.com/spf13/cobra"
)

// InstrumentInfo holds the regex pattern and description for an instrument
type InstrumentInfo struct {
	Regex       string
	Description []string
}

// RnameOutputData holds the parsed information for a single rname
type RnameOutputData struct {
	InputName      string
	InstrumentID   string
	InstrumentType string
	InstrumentRun  string
	FlowcellID     string
	FlowcellType   string
	LaneID         string
	ErrorParsing   error
}

// Instrument and flow cell information
var InstrumentIDs = []InstrumentInfo{
	{"HWUSI", []string{"Genome Analyzer IIx"}},
	{"HWI-M[0-9]{4}", []string{"MiSeq"}},
	{"M[0-9]{5}", []string{"MiSeq"}},
	{"HWI-C[0-9]{5}", []string{"HiSeq 1500"}},
	{"C[0-9]{5}", []string{"HiSeq 1500"}},
	{"HWI-D[0-9]{5}", []string{"HiSeq 2500"}},
	{"D[0-9]{5}", []string{"HiSeq 2500"}},
	{"J[0-9]{5}", []string{"HiSeq 3000"}},
	{"K[0-9]{5}", []string{"HiSeq 3000", "HiSeq 4000"}},
	{"E[0-9]{5}", []string{"HiSeq X"}},
	{"NB[0-9]{6}", []string{"NextSeq 500/550"}},
	{"NS[0-9]{6}", []string{"NextSeq 500/550"}},
	{"VH[0-9]{5}", []string{"NextSeq 2000"}},
	{"MN[0-9]{5}", []string{"MiniSeq"}},
	{"A[0-9]{5}", []string{"NovaSeq"}},
	{"NA[0-9]{5}", []string{"NovaSeq"}},
	{"LH[0-9]{5}", []string{"NovaSeq X"}},
	{"SN[0-9]{3}", []string{"HiSeq2000", "HiSeq2500"}},
	{".*", []string{"Unknown"}},
}

var FCIDs = []InstrumentInfo{
	{"BNT[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BRB[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BPC[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BPG[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BPA[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BPL[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"BTR[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100 Standard Output"}},
	{"000H[A-Z0-9]{5}", []string{"MiniSeq", "Mid or High Output"}},
	{"D[A-Z0-9]{4}", []string{"MiSeq Nano"}},
	{"G[A-Z0-9]{4}", []string{"MiSeq Micro"}},
	{"A[A-Z0-9]{4}", []string{"MiSeq Standard v2"}},
	{"B[A-Z0-9]{4}", []string{"MiSeq Standard"}},
	{"C[A-Z0-9]{4}", []string{"MiSeq Standard"}},
	{"J[A-Z0-9]{4}", []string{"MiSeq Standard"}},
	{"K[A-Z0-9]{4}", []string{"MiSeq Standard"}},
	{"L[A-Z0-9]{4}", []string{"MiSeq Standard"}},
	{"[A-Z0-9]{5}AF[A-Z0-9]{2}", []string{"NextSeq 500/550 Mid Output"}},
	{"[A-Z0-9]{5}AG[A-Z0-9]{2}", []string{"NextSeq 500/550 High Output"}},
	{"[A-Z0-9]{5}BG[A-Z0-9]{2}", []string{"NextSeq 500/550 High Output"}},
	{"[A-Z0-9]{7}M5", []string{"NextSeq 1000/2000 P1 or P2"}},
	{"[A-Z0-9]{7}HV", []string{"NextSeq 1000/2000 P3"}},
	{"[A-Z0-9]{7}NX", []string{"NextSeq 1000/2000 XLEAP-SBS P4"}},
	{"[A-Z0-9]{5}BC[A-Z0-9]{2}", []string{"HiSeq 2500", "Rapid Run (2-lane) v2"}},
	{"[A-Z0-9]{5}AC[A-Z0-9]{2}", []string{"HiSeq 2500", "TrueSeq v3"}},
	{"[A-Z0-9]{5}AN[A-Z0-9]{2}", []string{"HiSeq 2500", "High Output v3"}},
	{"[A-Z0-9]{5}BB[A-Z0-9]{2}", []string{"HiSeq 3000", "HiSeq 4000", "(8-lane) v1"}},
	{"[A-Z0-9]{5}AL[A-Z0-9]{2}", []string{"HiSeq X", "(8-lane)"}},
	{"[A-Z0-9]{5}CC[A-Z0-9]{2}", []string{"HiSeq X", "(8-lane)"}},
	{"[A-Z0-9]{5}DR[A-Z0-9]{2}", []string{"NovaSeq 6000 SP or S1"}},
	{"[A-Z0-9]{5}DM[A-Z0-9]{2}", []string{"NovaSeq 6000 S2"}},
	{"[A-Z0-9]{5}DS[A-Z0-9]{2}", []string{"NovaSeq 6000 S4"}},
	{"[A-Z0-9]{6}LT3", []string{"NovaSeq X Plus 10B"}},
	{"[A-Z0-9]{6}LT4", []string{"NovaSeq X Plus 25B"}},
	{"[A-Z0-9]{6}LT[A-Z0-9]", []string{"NovaSeq X", "NovaSeq X Plus", "Unknown flow cell"}},
	{".*", []string{"Unknown Machine", "Unknown flow cell"}},
}

var (
	prettyPrint bool // Flag for table output
	rnameCmd    = &cobra.Command{
		Use:   "rname [file/rname_string ...]",
		Short: "Identify instrument, flow cell type, and lane based on read names",
		Long: `This command takes one or more inputs which could be:
    - A filename of a FASTQ file (.gz or plain)
    - A direct input rname string
    - Reads from stdin if '-' is provided as an argument and data is piped.
It extracts the first record name from each input and identifies the instrument, 
flow cell type, and lane. Supports multiple inputs and different output formats.`,
		Args: cobra.MinimumNArgs(1), // Requires at least one argument
		Run: func(cmd *cobra.Command, args []string) {
			var allResults []RnameOutputData

			for _, inputArg := range args {
				currentData := RnameOutputData{InputName: inputArg}
				rname, err := extractRname(inputArg)
				if err != nil {
					currentData.ErrorParsing = fmt.Errorf("error extracting rname from '%s': %w", inputArg, err)
					allResults = append(allResults, currentData)
					continue
				}

				inputParts := strings.Split(rname, ":")
				if len(inputParts) < 3 {
					currentData.ErrorParsing = fmt.Errorf("invalid rname format in '%s': %s (expected at least 3 colon-separated parts)", inputArg, rname)
					allResults = append(allResults, currentData)
					continue
				}

				currentData.InstrumentID = inputParts[0]
				currentData.InstrumentRun = inputParts[1]
				currentData.FlowcellID = inputParts[2]
				currentData.LaneID = "N/A"
				if len(inputParts) >= 4 {
					currentData.LaneID = inputParts[3]
				}

				currentData.InstrumentType = printInstrumentType(currentData.InstrumentID)
				currentData.FlowcellType = printFlowCellType(currentData.FlowcellID)
				allResults = append(allResults, currentData)
			}

			outputResults(allResults, prettyPrint)
		},
	}
)

func outputResults(results []RnameOutputData, usePrettyTable bool) {
	if len(results) == 0 {
		fmt.Println("No results to display.")
		return
	}

	if usePrettyTable {
		// Pretty table output for single or multiple results
		t := table.New(os.Stdout)
		t.SetHeaders("Input", "Instrument ID", "Type", "Run", "Flowcell ID", "Type", "Lane", "Status")
		t.SetHeaderStyle(table.StyleBold)
		t.SetLineStyle(table.StyleBlue)
		t.SetDividers(table.UnicodeRoundedDividers)
		t.SetAutoMerge(false) // Keep cells separate

		for _, res := range results {
			status := "OK"
			if res.ErrorParsing != nil {
				status = fmt.Sprintf("Error: %v", res.ErrorParsing)
				// For table output, show N/A for fields if error occurred early
				if res.InstrumentID == "" {
					res.InstrumentID = "N/A"
				}
				if res.InstrumentType == "" {
					res.InstrumentType = "N/A"
				}
				if res.InstrumentRun == "" {
					res.InstrumentRun = "N/A"
				}
				if res.FlowcellID == "" {
					res.FlowcellID = "N/A"
				}
				if res.FlowcellType == "" {
					res.FlowcellType = "N/A"
				}
				if res.LaneID == "" {
					res.LaneID = "N/A"
				}
			}
			t.AddRow(
				res.InputName,
				res.InstrumentID,
				res.InstrumentType,
				res.InstrumentRun,
				res.FlowcellID,
				res.FlowcellType,
				res.LaneID,
				status,
			)
		}
		t.Render()
	} else if len(results) > 1 {
		// TSV output for multiple results (default)
		fmt.Println("Input\tInstrumentID\tInstrumentType\tRun\tFlowcellID\tFlowcellType\tLane\tStatus\tErrorMessage")
		for _, res := range results {
			status := "OK"
			errMsg := ""
			if res.ErrorParsing != nil {
				status = "Error"
				errMsg = strings.ReplaceAll(res.ErrorParsing.Error(), "\t", " ") // Sanitize error message for TSV
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				res.InputName,
				res.InstrumentID,
				res.InstrumentType,
				res.InstrumentRun,
				res.FlowcellID,
				res.FlowcellType,
				res.LaneID,
				status,
				errMsg,
			)
		}
	} else {
		// Original line-by-line output for a single result (default)
		res := results[0]
		if res.ErrorParsing != nil {
			fmt.Printf("Error processing input '%s': %v\n", res.InputName, res.ErrorParsing)
		} else {
			fmt.Printf("Input          : %s\n", res.InputName)
			fmt.Printf("Instrument ID  : %s ➜ %s\n", res.InstrumentID, res.InstrumentType)
			fmt.Printf("Instrument Run : %s\n", res.InstrumentRun)
			fmt.Printf("Flow cell ID   : %s ➜ %s\n", res.FlowcellID, res.FlowcellType)
			fmt.Printf("Lane ID        : %s\n", res.LaneID)
		}
	}
}

func extractRname(inputArg string) (string, error) {
	var reader io.Reader
	isStdin := inputArg == "-"

	if isStdin {
		// Check if data is being piped to stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			reader = os.Stdin
		} else {
			return "", fmt.Errorf("asked to read from stdin ('-') but no data was piped")
		}
	} else {
		// Check if the input is an existing file
		if fileInfo, err := os.Stat(inputArg); err == nil && !fileInfo.IsDir() {
			file, errOpen := os.Open(inputArg)
			if errOpen != nil {
				return "", fmt.Errorf("failed to open file '%s': %w", inputArg, errOpen)
			}
			defer file.Close() // Ensure file is closed after this function, not just os.Open scope

			if strings.HasSuffix(strings.ToLower(inputArg), ".gz") {
				gzipReader, errGzip := gzip.NewReader(file)
				if errGzip != nil {
					return "", fmt.Errorf("failed to open gzip file '%s': %w", inputArg, errGzip)
				}
				// defer gzipReader.Close() // gzipReader is closed when file is closed
				reader = gzipReader
			} else {
				reader = file
			}
		} else if os.IsNotExist(err) {
			// If the file does not exist, treat inputArg as a direct rname string
			rname := strings.TrimPrefix(inputArg, "@")
			parts := strings.Fields(rname) // Handle cases like "@rname extra_info"
			if len(parts) > 0 {
				return parts[0], nil
			}
			return "", fmt.Errorf("empty rname string provided: '%s'", inputArg)
		} else if err != nil { // Other stat error
			return "", fmt.Errorf("error accessing '%s': %w", inputArg, err)
		} else if fileInfo.IsDir() { // It's a directory
			return "", fmt.Errorf("input '%s' is a directory, not a file or rname string", inputArg)
		}
	}

	// If reader is set (either from file or stdin)
	if reader != nil {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimPrefix(line, "@")
			parts := strings.Fields(line) // Handle cases like "rname extra_info" from file line
			if len(parts) > 0 {
				return parts[0], nil
			}
			return "", fmt.Errorf("empty line read from input source '%s'", inputArg)
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("error scanning input from '%s': %w", inputArg, err)
		}
		return "", fmt.Errorf("no data read from input source '%s'", inputArg)
	}

	// Fallback for direct rname string if not caught earlier (should be rare with current logic)
	// This primarily handles the case where inputArg was not a file and not stdin
	if !isStdin {
		rname := strings.TrimPrefix(inputArg, "@")
		parts := strings.Fields(rname)
		if len(parts) > 0 {
			return parts[0], nil
		}
		return "", fmt.Errorf("invalid or empty rname string provided: '%s'", inputArg)
	}

	return "", fmt.Errorf("unable to determine input type or read data for '%s'", inputArg)
}

func printInstrumentType(instrumentID string) string {
	if instrumentID == "" || instrumentID == "N/A" {
		return "N/A"
	}
	for _, instrument := range InstrumentIDs {
		regex, err := regexp.Compile("^" + instrument.Regex + "$")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error compiling instrument regex '%s': %v\n", instrument.Regex, err)
			continue
		}
		if regex.MatchString(instrumentID) {
			return strings.Join(instrument.Description, ", ")
		}
	}
	return "Unknown"
}

func printFlowCellType(flowcellID string) string {
	if flowcellID == "" || flowcellID == "N/A" {
		return "N/A"
	}
	for _, fcid := range FCIDs {
		regex, err := regexp.Compile("^" + fcid.Regex + "$")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error compiling flow cell regex '%s': %v\n", fcid.Regex, err)
			continue
		}
		if regex.MatchString(flowcellID) {
			return strings.Join(fcid.Description, ", ")
		}
	}
	return "Unknown"
}

func init() {
	rootCmd.AddCommand(rnameCmd)
	rnameCmd.Flags().BoolVarP(&prettyPrint, "pretty", "p", false, "Output in a pretty table format (applies to single or multiple inputs)")
}
