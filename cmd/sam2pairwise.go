package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/signal" // Needed for signal handling
	"strconv"
	"strings"
	"syscall" // Needed for signal handling

	"github.com/liamg/tml" // Import tml for coloring
	"github.com/spf13/cobra"
)

// --- Added flag variables ---
var (
	knownMutation     string
	knownMutationMark string
	filterForward     bool // Flag for -f
	filterReverse     bool // Flag for -r
)

// sam2pairwiseCmd processes SAM records from stdin into pairwise alignments
var sam2pairwiseCmd = &cobra.Command{
	Use:   "sam2pairwise [-m REF>ALT] [-l MARK] [-f] [-r]",
	Short: "Convert SAM records from stdin into pairwise alignment format",
	Long: `Processes SAM records, parsing CIGAR and MD tags to generate pairwise alignments.
Highlighting: Mismatches = colored background; Matches = plain text.

Filtering Options:
  -f, --forward: Only process Read 1 Forward or Read 2 Reverse reads.
  -r, --reverse: Only process Read 1 Reverse or Read 2 Forward reads.
  (If neither -f nor -r is specified, all reads are processed).

Marking Mismatches:
  Optionally, use -m REF>ALT (e.g., -m C>T) and -l MARK (e.g., -l '.')
  to mark specific known mismatches with MARK instead of a space.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Validate the knownMutationMark (must be a single character)
		if len(knownMutationMark) > 1 {
			fmt.Fprintln(os.Stderr, "Error: -l mark must be a single character.")
			os.Exit(1)
		}
		// Validate knownMutation format if provided
		if knownMutation != "" && (len(knownMutation) != 3 || knownMutation[1] != '>') {
			fmt.Fprintln(os.Stderr, "Error: -m mutation format must be REF>ALT (e.g., C>T).")
			os.Exit(1)
		}
		// Validate filter flags
		if filterForward && filterReverse {
			fmt.Fprintln(os.Stderr, "Error: Cannot use -f and -r flags simultaneously.")
			os.Exit(1)
		}
		processSAMStdin()
	},
}

func init() {
	rootCmd.AddCommand(sam2pairwiseCmd)
	// --- Define flags ---
	sam2pairwiseCmd.Flags().StringVarP(&knownMutation, "mutation", "m", "", "Known mutation to mark (e.g., C>T)")
	sam2pairwiseCmd.Flags().StringVarP(&knownMutationMark, "mark", "l", ".", "Single character to use for marking the known mutation")
	sam2pairwiseCmd.Flags().BoolVarP(&filterForward, "forward", "f", false, "Filter for Read 1 Forward or Read 2 Reverse")
	sam2pairwiseCmd.Flags().BoolVarP(&filterReverse, "reverse", "r", false, "Filter for Read 1 Reverse or Read 2 Forward")
}

// processSAMStdin reads SAM records from stdin and converts each record into pairwise alignment
func processSAMStdin() {
	// --- Signal Handling Setup ---
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, syscall.SIGINT, syscall.SIGTERM) // Listen for Ctrl+C (SIGINT) and SIGTERM
	continueProcessing := true                                    // Flag to control the main loop

	// Goroutine to handle the signal
	go func() {
		<-interruptChan // Wait for a signal
		//tml.Printf("\n<yellow><bold>Signal received. Finishing current record and exiting...</bold></yellow>\n")
		continueProcessing = false // Signal the main loop to stop
	}()
	// --- End Signal Handling Setup ---

	scanner := bufio.NewScanner(os.Stdin)

	// --- Pre-parse known mutation ---
	var knownRefBase, knownAltBase byte
	useKnownMutation := false
	if knownMutation != "" {
		knownRefBase = knownMutation[0]
		knownAltBase = knownMutation[2]
		useKnownMutation = true
	}
	// Use the first character of the mark string, default '.'
	markChar := []rune(knownMutationMark)[0]

	// --- Main Processing Loop ---
	for continueProcessing && scanner.Scan() { // Check continueProcessing flag here
		line := scanner.Text()
		if strings.HasPrefix(line, "@") {
			continue // Skip header lines
		}

		fields := strings.Fields(line)
		if len(fields) < 11 {
			fmt.Fprintln(os.Stderr, "Skipping invalid SAM record (less than 11 fields):", line)
			continue
		}

		// Extract necessary fields
		readName := fields[0]
		flagStr := fields[1] // Keep flag as string for printing
		refName := fields[2] // Reference sequence name
		pos := fields[3]     // 1-based leftmost mapping POSition
		cigar := fields[5]   // CIGAR string
		seq := fields[9]     // Segment SEQuence
		mdTagValue := ""     // Initialize MD tag value

		// --- Filtering Logic ---
		if filterForward || filterReverse {
			flag, err := strconv.Atoi(flagStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Skipping invalid SAM record (invalid flag %s): %s\n", flagStr, line)
				continue
			}

			isRead1 := (flag & 0x40) != 0
			isRead2 := (flag & 0x80) != 0
			isReverse := (flag & 0x10) != 0

			if filterForward {
				// Keep Read 1 Forward OR Read 2 Reverse
				if !((isRead1 && !isReverse) || (isRead2 && isReverse)) {
					continue // Skip this read
				}
			} else if filterReverse {
				// Keep Read 1 Reverse OR Read 2 Forward
				if !((isRead1 && isReverse) || (isRead2 && !isReverse)) {
					continue // Skip this read
				}
			}
		}
		// --- End Filtering Logic ---

		// Find and extract the MD tag *value*
		for _, field := range fields[11:] {
			if strings.HasPrefix(field, "MD:Z:") {
				mdTagValue = field[5:]
				break
			}
		}

		refSeq, alignedSeq, markers, err := samToPairwise(seq, cigar, mdTagValue, useKnownMutation, knownRefBase, knownAltBase, markChar)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing read %s: %v\n", readName, err)
			continue // Skip this read and proceed to the next
		}

		// --- Header Line Output (User Requested Format) ---
		tml.Printf("<darkgrey><italic>%s\t%s\t%s\t%s\t%s\t%s</italic></darkgrey>\n", readName, flagStr, refName, pos, cigar, mdTagValue)
		// --- End of Header Line ---

		// Print alignment strings using tml.Println to render colors
		tml.Printf(alignedSeq + "\n")
		fmt.Println(markers)
		tml.Printf(refSeq + "\n")
		fmt.Println() // Blank line separator
	} // --- End Main Processing Loop ---

	if err := scanner.Err(); err != nil {
		// Avoid printing error if we stopped due to interrupt
		if continueProcessing {
			fmt.Fprintln(os.Stderr, "Error reading standard input:", err)
		}
	}

	// If loop finished because of signal, indicate graceful exit
	if !continueProcessing {
		//fmt.Fprintln(os.Stderr, "Processing finished.")
		tml.Fprintln(os.Stderr, "<yellow><bold>Signal received. Finishing current record and exiting...</bold></yellow>")
	}
}

// MDTagEntry represents an entry in the parsed MD tag
type MDTagEntry struct {
	Num     int    // Number of matching bases
	Changes string // Mismatches or deletions (bases from reference)
	IsDel   bool   // Is this a deletion?
}

// parseMDTag parses the MD tag into a structured list of MDTagEntry objects
// Revised for more robust state handling and end-of-string checks.
func parseMDTag(mdTag string) ([]MDTagEntry, error) {
	var entries []MDTagEntry
	var numStr strings.Builder
	var changeStr strings.Builder
	state := "num" // States: num, change, del_start, del

	if mdTag == "" {
		return entries, nil
	}

	// Helper to add number entry if numStr is not empty
	addNumEntry := func() error {
		if numStr.Len() > 0 {
			num, err := strconv.Atoi(numStr.String())
			if err != nil {
				return fmt.Errorf("invalid number '%s' in MD tag", numStr.String())
			}
			// Allow Num 0, it's valid, means zero matches before next op
			entries = append(entries, MDTagEntry{Num: num})
			numStr.Reset()
		}
		return nil
	}

	// Helper to add deletion entry
	addDelEntry := func() error {
		if changeStr.Len() > 0 {
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: true})
			changeStr.Reset()
			return nil
		}
		return fmt.Errorf("empty deletion sequence found in MD tag") // Deletion must have bases
	}

	// Helper to add mismatch entry
	addMismatchEntry := func() error {
		if changeStr.Len() == 1 { // Mismatch must be single base
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: false, Num: 0})
			changeStr.Reset()
			return nil
		}
		return fmt.Errorf("invalid mismatch sequence '%s' (must be 1 base) in MD tag", changeStr.String())
	}

	for i, char := range mdTag {
		isLastChar := (i == len(mdTag)-1)

		switch state {
		case "num":
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
			} else {
				// End of number, process it
				if err := addNumEntry(); err != nil {
					return nil, err
				}
				// Start processing the non-digit character
				if char == '^' {
					state = "del_start"
				} else if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
					changeStr.WriteRune(char)
					state = "change" // Expecting next char to determine end of mismatch
				} else {
					return nil, fmt.Errorf("unexpected character '%c' after number in MD tag at position %d", char, i)
				}
			}
		case "change": // Just read a mismatch base
			if err := addMismatchEntry(); err != nil {
				return nil, err
			}
			// Now process the character *after* the mismatch base
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
				state = "num"
			} else if char == '^' {
				state = "del_start"
			} else {
				return nil, fmt.Errorf("unexpected character '%c' after mismatch base in MD tag at position %d", char, i)
			}
		case "del_start": // Just read '^'
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
				state = "del" // Start accumulating deletion bases
			} else {
				return nil, fmt.Errorf("expected base after deletion '^' in MD tag, got '%c' at position %d", char, i)
			}
		case "del": // Accumulating deletion bases
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
				// Stay in del state
			} else { // End of deletion sequence
				if err := addDelEntry(); err != nil {
					return nil, err
				}
				// Process the character *after* the deletion sequence
				if char >= '0' && char <= '9' {
					numStr.WriteRune(char)
					state = "num"
				} else if char == '^' {
					state = "del_start"
				} else {
					return nil, fmt.Errorf("unexpected character '%c' after deletion sequence in MD tag at position %d", char, i)
				}
			}
		}

		// If it's the last character, handle any pending state
		if isLastChar {
			switch state {
			case "num":
				if err := addNumEntry(); err != nil {
					return nil, err
				}
			case "change": // Cannot end after mismatch base
				return nil, fmt.Errorf("MD tag cannot end after a mismatch base")
			case "del":
				if err := addDelEntry(); err != nil {
					return nil, err
				}
			case "del_start": // Cannot end with ^
				return nil, fmt.Errorf("MD tag cannot end with deletion start '^'")
			}
		}
	}

	return entries, nil
}

// samToPairwise function remains the same as provided in the context
// --- Updated function to build colored strings directly ---
func samToPairwise(seq string, cigar string, mdTag string, useKnownMutation bool, knownRefBase byte, knownAltBase byte, markChar rune) (refSeqColored string, alignedSeqColored string, markers string, err error) {
	var refBuilder, alignedSeqBuilder, markerBuilder strings.Builder
	seqPos := 0 // Current position in the input sequence string

	// CIGAR parsing
	cigarOps, err := parseCigar(cigar)
	if err != nil {
		return "", "", "", fmt.Errorf("error parsing CIGAR '%s': %w", cigar, err)
	}

	// MD tag parsing (only if MD tag exists)
	var mdEntries []MDTagEntry
	mdIndex := 0  // Current position in mdEntries
	mdSubPos := 0 // Position within the current MD entry (e.g., within a number or deletion string)
	hasMD := mdTag != ""
	mdParseErr := false // Flag if MD parsing failed
	if hasMD {
		mdEntries, err = parseMDTag(mdTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error parsing MD tag '%s', proceeding without MD info: %v\n", mdTag, err)
			hasMD = false
			mdParseErr = true // Remember parsing failed
			mdEntries = nil   // Discard potentially partial/invalid entries
		}
	}

	// Helper function for consistent coloring logic - UPDATED SCHEME
	applyColor := func(builder *strings.Builder, base byte, isMismatch bool) {
		tagOpen := ""
		tagClose := ""
		color := "" // Only used for background on mismatch

		// Determine base color (only needed for mismatch background)
		switch base {
		case 'A', 'a':
			color = "red"
		case 'T', 't':
			color = "green"
		case 'G', 'g':
			color = "yellow"
		case 'C', 'c':
			color = "blue"
		}

		// Determine formatting based on character type and mismatch status
		switch base {
		case 'A', 'a', 'T', 't', 'G', 'g', 'C', 'c':
			if isMismatch { // Mismatch: Apply background color
				tagOpen = "<bg-" + color + ">"
				tagClose = "</bg-" + color + ">"
				builder.WriteString(tagOpen)
				builder.WriteByte(base)
				builder.WriteString(tagClose)
			} else { // Match: Write plain text
				builder.WriteByte(base) // <-- NO COLOR TAGS FOR MATCHES
			}
		case '-': // Gap
			builder.WriteString(tml.Sprintf("<bg-black>%c</bg-black>", base))
		case 'N', 'n': // Ambiguous / Clipped Ref / Skipped Ref
			builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
		case '.': // Skipped Read
			builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
		case '*': // Padding
			builder.WriteString(tml.Sprintf("<bg-magenta>%c</bg-magenta>", base))
		default: // Other characters
			builder.WriteByte(base)
		}
	}

	// Process CIGAR operations
	for _, op := range cigarOps {
		length := op.Length
		opType := op.Op

		switch opType {
		case 'M', '=', 'X': // Match, Sequence match, Sequence mismatch
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (M/=/X op)")
				}
				readBase := seq[seqPos]
				var refBase byte = readBase // Assume match initially
				isMismatch := false

				if hasMD {
					// Skip potential {Num: 0} entries before processing this CIGAR base
					for hasMD && mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}

					// Check if we still have valid MD entry after potentially skipping Num:0
					if mdIndex >= len(mdEntries) {
						if !mdParseErr {
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during M/=/X op%s. Assuming 'N' for reference base.\n",
								func() string {
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						hasMD = false // Stop trusting MD tag
					}

					if hasMD {
						currentMdEntry := &mdEntries[mdIndex]
						if currentMdEntry.IsDel {
							// This indicates CIGAR/MD inconsistency
							return "", "", "", fmt.Errorf("MD tag indicates deletion (^) during CIGAR match/mismatch (M/=/X) operation at MD index %d (MD: %s)", mdIndex, mdTag)
						}

						if currentMdEntry.Num > 0 { // Match block in MD
							refBase = readBase
							isMismatch = false
							mdSubPos++
							if mdSubPos == currentMdEntry.Num { // Consumed this match block
								mdIndex++
								mdSubPos = 0
							}
						} else { // Mismatch block in MD (Num is 0, Changes has ref base)
							if len(currentMdEntry.Changes) != 1 {
								// This should ideally be caught by parseMDTag, but double-check
								fmt.Fprintf(os.Stderr, "Warning: Internal logic error or invalid MD? Mismatch entry has len != 1 ('%s') in MD tag '%s'. Treating reference base as 'N'.\n", currentMdEntry.Changes, mdTag)
								refBase = 'N'
								isMismatch = true
							} else {
								refBase = currentMdEntry.Changes[0]
								isMismatch = true
							}
							// Mismatch entry is consumed entirely in one step
							mdIndex++
							mdSubPos = 0 // Reset subpos for next entry
						}
					} else { // No valid MD info left
						refBase = 'N'
						isMismatch = true
					}

				} else if opType == 'X' || (opType == 'M' && !hasMD) { // No MD tag available
					refBase = 'N'
					isMismatch = true
				}
				// If CIGAR op is '=', it implies a match, refBase=readBase, isMismatch=false (already default)

				// Apply coloring based on mismatch status
				applyColor(&alignedSeqBuilder, readBase, isMismatch)
				applyColor(&refBuilder, refBase, isMismatch)

				// Determine marker
				if isMismatch {
					if useKnownMutation && refBase == knownRefBase && readBase == knownAltBase {
						markerBuilder.WriteRune(markChar)
					} else {
						markerBuilder.WriteByte(' ')
					}
				} else {
					markerBuilder.WriteByte('|')
				}
				seqPos++
			} // end inner loop for length

		case 'I': // Insertion to the reference
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (I op)")
				}
				readBase := seq[seqPos]
				applyColor(&alignedSeqBuilder, readBase, true) // Highlight inserted base in read
				applyColor(&refBuilder, '-', true)             // Gap in ref
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'D': // Deletion from the reference
			if !hasMD {
				if !mdParseErr { // Only warn if MD wasn't already known bad
					fmt.Fprintf(os.Stderr, "Warning: Deletion (D) in CIGAR but no valid MD tag. Representing deleted reference bases as 'N'.\n")
				}
				for range length { // Modernized loop
					applyColor(&alignedSeqBuilder, '-', true) // Gap in read
					applyColor(&refBuilder, 'N', true)        // Unknown deleted base
					markerBuilder.WriteByte(' ')
				}
			} else {
				// Use MD tag to find deleted bases
				deletedBasesFound := 0
				for deletedBasesFound < length {
					// Skip potential {Num: 0} entries before checking IsDel
					for hasMD && mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}

					if mdIndex >= len(mdEntries) {
						if !mdParseErr { // Don't warn again if MD parsing failed initially
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during D op%s. Representing remaining deleted bases as 'N'.\n",
								func() string {
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						// Fill remaining needed bases with 'N'
						for k := 0; k < length-deletedBasesFound; k++ {
							applyColor(&alignedSeqBuilder, '-', true) // Gap in read
							applyColor(&refBuilder, 'N', true)        // Unknown deleted base
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound = length // Mark as done
						hasMD = false              // Stop trusting MD tag
						break                      // Break inner MD processing loop
					}

					currentMdEntry := &mdEntries[mdIndex]

					// Now check IsDel *after* skipping Num:0
					if currentMdEntry.IsDel {
						delSeqLen := len(currentMdEntry.Changes)
						basesAvailable := delSeqLen - mdSubPos
						basesNeeded := length - deletedBasesFound
						basesToTake := min(basesNeeded, basesAvailable)

						for i := range basesToTake { // Modernized loop
							delBase := currentMdEntry.Changes[mdSubPos+i]
							applyColor(&alignedSeqBuilder, '-', true) // Gap in read
							applyColor(&refBuilder, delBase, true)    // Show deleted base from ref with mismatch highlight
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound += basesToTake
						mdSubPos += basesToTake

						if mdSubPos == delSeqLen { // Consumed this deletion block
							mdIndex++
							mdSubPos = 0
						}
					} else {
						// Error: MD/CIGAR mismatch
						return "", "", "", fmt.Errorf("MD tag indicates match/mismatch (Num: %d, Changes: '%s') during CIGAR deletion (D) operation at MD index %d (MD: %s)", currentMdEntry.Num, currentMdEntry.Changes, mdIndex, mdTag)
					}
				} // end while deletedBasesFound < length
			}
		case 'N': // Skipped region from the reference (e.g., intron)
			for range length { // Modernized loop
				applyColor(&alignedSeqBuilder, '.', false) // Use darkgrey foreground for skipped read part
				applyColor(&refBuilder, 'N', false)        // Use darkgrey foreground for skipped ref part
				markerBuilder.WriteByte(' ')
				// Note: seqPos does NOT advance for N operation
			}
		case 'S': // Soft clipping (clipped sequences present in SEQ)
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (S op)")
				}
				readBase := seq[seqPos]
				applyColor(&alignedSeqBuilder, readBase, true) // Highlight soft clipped read bases
				applyColor(&refBuilder, 'N', false)            // Indicate non-aligning ref part
				markerBuilder.WriteByte(' ')
				seqPos++
			}

		case 'H': // Hard clipping (clipped sequences NOT in SEQ)
			continue // Do nothing, seqPos does not advance
		case 'P': // Padding (silent deletion from padded reference)
			for range length { // Modernized loop
				applyColor(&alignedSeqBuilder, '*', true) // Use magenta background for padding
				applyColor(&refBuilder, '*', true)
				markerBuilder.WriteByte(' ')
				// seqPos does NOT advance for P operation
			}
		default:
			return "", "", "", fmt.Errorf("unsupported CIGAR operation: %c", opType)
		}
	} // end for each cigarOp

	// Final Check: Ensure sequence length matches CIGAR operations that consume sequence
	expectedSeqLen := 0
	for _, op := range cigarOps {
		if strings.ContainsRune("MIS=X", op.Op) { // Operations consuming sequence
			expectedSeqLen += op.Length
		}
	}
	if seqPos != len(seq) && len(seq) > 0 {
		// Allow seqPos < len(seq) if MD tag parsing failed, as it might cause early termination of processing within M/=/X ops
		if seqPos < len(seq) && !mdParseErr {
			fmt.Fprintf(os.Stderr, "Warning: CIGAR operations consumed %d bases, but sequence length is %d. Result might be truncated or CIGAR/SEQ inconsistent.\n", seqPos, len(seq))
		} else if seqPos > len(seq) { // seqPos > len(seq) is always an issue
			fmt.Fprintf(os.Stderr, "Error: CIGAR operations consumed %d bases, but sequence length is only %d. CIGAR/SEQ inconsistent.\n", seqPos, len(seq))
			// Optionally return an error here if strict consistency is required
			// return "", "", "", fmt.Errorf("CIGAR/SEQ length mismatch: CIGAR implies %d bases, SEQ has %d", seqPos, len(seq))
		}
	}

	// Return the fully built, colored strings
	return refBuilder.String(), alignedSeqBuilder.String(), markerBuilder.String(), nil
}

// parseCigar parses a CIGAR string and returns a slice of CigarOps
func parseCigar(cigar string) ([]CigarOp, error) {
	if cigar == "*" { // Handle unmapped reads
		return []CigarOp{}, nil
	}
	var ops []CigarOp
	var lengthStr strings.Builder

	for _, char := range cigar {
		if char >= '0' && char <= '9' {
			lengthStr.WriteRune(char)
		} else if strings.ContainsRune("MIDNSHP=X", char) { // Check if it's a valid CIGAR operation type
			if lengthStr.Len() == 0 {
				// Allow operation at the beginning without length (implicit length 1)
				// This is non-standard but might occur? Usually not.
				// Let's treat it as an error for standard compliance.
				return nil, fmt.Errorf("CIGAR operation '%c' has no preceding length", char)
			}
			length, err := strconv.Atoi(lengthStr.String())
			if err != nil || length <= 0 { // Length must be > 0
				return nil, fmt.Errorf("invalid CIGAR length '%s' for operation '%c'", lengthStr.String(), char)
			}
			ops = append(ops, CigarOp{
				Length: length,
				Op:     char,
			})
			lengthStr.Reset() // Reset for the next operation
		} else {
			return nil, fmt.Errorf("invalid character '%c' in CIGAR string", char)
		}
	}
	// Check if there's a trailing number without an operation
	if lengthStr.Len() > 0 {
		return nil, fmt.Errorf("CIGAR string ends with an incomplete operation (number '%s' without type)", lengthStr.String())
	}

	return ops, nil
}

// CigarOp represents a parsed CIGAR operation
type CigarOp struct {
	Length int
	Op     rune
}

// min function (requires Go 1.21+)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max function (requires Go 1.21+) - Not used currently but good to have
// func max(a, b int) int {
// 	if a > b {
// 		return a
// 	}
// 	return b
// }
