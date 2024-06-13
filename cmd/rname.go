package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// InstrumentInfo holds the regex pattern and description for an instrument
type InstrumentInfo struct {
	Regex       string
	Description []string
}

var InstrumentIDs = []InstrumentInfo{
	// Include the InstrumentIDs here as per the previous listing
	{"HWI-M[0-9]{4}", []string{"MiSeq"}},
	{"HWUSI", []string{"Genome Analyzer IIx"}},
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
	{"SN[0-9]{3}", []string{"HiSeq2000", "HiSeq2500"}},
	{".*", []string{"Unknown"}},
}

var FCIDs = []InstrumentInfo{
	// Include the FCIDs here as per the previous listing
	// iSeq 100 standard output flow cells
	{"BNT[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BRB[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BPC[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BPG[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BPA[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BPL[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},
	{"BTR[A-Z0-9]{5}-[A-Z0-9]{4}", []string{"iSeq 100", "Standard Output flow cell"}},

	// MiniSeq output types
	{"000H[A-Z0-9]{5}", []string{"MiniSeq", "Mid or High Output flow cell"}},

	// MiSeq specific flow cells
	{"D[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Nano flow cell"}},
	{"G[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Micro flow cell"}},
	{"A[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard v2 flow cell"}},
	{"B[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard flow cell"}},
	{"C[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard flow cell"}},
	{"J[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard flow cell"}},
	{"K[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard flow cell"}},
	{"L[A-Z0-9]{4}", []string{"MiSeq", "MiSeq Standard flow cell"}},

	// NextSeq specific flow cells
	{"[A-Z0-9]{5}AF[A-Z0-9]{2}", []string{"NextSeq 500", "NextSeq 550", "Mid Output flow cell"}},
	{"[A-Z0-9]{5}AG[A-Z0-9]{2}", []string{"NextSeq 500", "NextSeq 550", "High Output flow cell"}},
	{"[A-Z0-9]{5}BG[A-Z0-9]{2}", []string{"NextSeq 500", "NextSeq 550", "High Output flow cell"}},
	{"[A-Z0-9]{7}M5", []string{"NextSeq 1000", "NextSeq 2000", "P1 or P2 flow cell"}},
	{"[A-Z0-9]{7}HV", []string{"NextSeq 1000", "NextSeq 2000", "P3 flow cell"}},
	{"[A-Z0-9]{7}NX", []string{"NextSeq 1000", "NextSeq 2000", "XLEAP-SBS P4 flow cell"}},

	// HiSeq specific flow cells
	{"[A-Z0-9]{5}BC[A-Z0-9]{2}", []string{"HiSeq 2500", "Rapid Run (2-lane) v2 flow cell"}},
	{"[A-Z0-9]{5}AC[A-Z0-9]{2}", []string{"HiSeq 2500", "TrueSeq v3 flow cell"}},
	{"[A-Z0-9]{5}AN[A-Z0-9]{2}", []string{"HiSeq 2500", "High Output v3 flow cell"}},
	{"[A-Z0-9]{5}BB[A-Z0-9]{2}", []string{"HiSeq 3000", "HiSeq 4000", "(8-lane) v1 flow cell"}},
	{"[A-Z0-9]{5}AL[A-Z0-9]{2}", []string{"HiSeq X", "(8-lane) flow cell"}},
	{"[A-Z0-9]{5}CC[A-Z0-9]{2}", []string{"HiSeq X", "(8-lane) flow cell"}},

	// NovaSeq specific flow cells
	{"[A-Z0-9]{5}DR[A-Z0-9]{2}", []string{"NovaSeq 6000", "SP or S1 flow cell"}},
	{"[A-Z0-9]{5}DM[A-Z0-9]{2}", []string{"NovaSeq 6000", "S2 flow cell"}},
	{"[A-Z0-9]{5}DS[A-Z0-9]{2}", []string{"NovaSeq 6000", "S4 flow cell"}},
	{"[A-Z0-9]{6}LT3", []string{"NovaSeq X", "NovaSeq X Plus", "10B flow cell"}},
	{"[A-Z0-9]{6}LT4", []string{"NovaSeq X", "NovaSeq X Plus", "25B flow cell"}},
	{"[A-Z0-9]{6}LT[A-Z0-9]", []string{"NovaSeq X", "NovaSeq X Plus", "Unknown flow cell"}},

	// Catch-all for unknown cases
	{".*", []string{"Unknown Machine", "Unknown flow cell"}},
}

// rnameCmd represents the rname command
var rnameCmd = &cobra.Command{
	Use:   "rname <instrumentID>:<instrumentRun>:<flowcellID>",
	Short: "Identify instrument and flow cell type based on IDs",
	Long: `This command takes a combined string of instrumentID and flowcellID separated by a hyphen (:) and identifies 
the instrument and flow cell type from predefined lists.`,
	Args: cobra.ExactArgs(1), // This command requires exactly one argument
	Run: func(cmd *cobra.Command, args []string) {
		input := args[0]

		if strings.HasPrefix(input, "@") {
			input = input[1:] // Remove the leading '@' if present
		}
		inputParts := strings.SplitN(input, ":", 3)
		if len(input) < 3 {
			fmt.Println("Invalid input format. Please enter as <instrumentID>:<instrumentRun>:<flowcellID>")
			return
		}

		instrumentID := inputParts[0]
		instrumentRun := inputParts[1]
		runID := inputParts[2]

		fmt.Println("Input:")
		fmt.Printf("Instrument ID: %s\n", instrumentID)
		fmt.Printf("Instrument Run: %s\n", instrumentRun)
		fmt.Printf("Run ID: %s\n", runID)
		fmt.Println()

		fmt.Println("Inferred:")
		printInstrumentType(instrumentID)
		fmt.Println("Instrument Run: ", instrumentRun)
		printFlowCellType(runID)
	},
}

func printInstrumentType(instrumentID string) {
	for _, instrument := range InstrumentIDs {
		match, _ := regexp.MatchString(instrument.Regex, instrumentID)
		if match {
			fmt.Printf("Instrument Type: %v\n", instrument.Description)
			return
		}
	}
	fmt.Println("Instrument Type: Unknown")
}

func printFlowCellType(runID string) {
	for _, fcid := range FCIDs {
		match, _ := regexp.MatchString(fcid.Regex, runID)
		if match {
			fmt.Printf("Flow Cell Type: %v\n", fcid.Description)
			return
		}
	}
	fmt.Println("Flow Cell Type: Unknown")
}

func init() {
	rootCmd.AddCommand(rnameCmd) // rootCmd is assumed to be defined in your Cobra application
}
