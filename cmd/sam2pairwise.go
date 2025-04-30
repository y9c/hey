package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var (
	knownMutation     string
	knownMutationMark string
	filterForward     bool
	filterReverse     bool
)

const (
	minIntronCompressLength = 20 // Minimum length of 'N' CIGAR op to compress
	condensedNSEdgeLength   = 5  // Number of Ns/dots on each side of compressed format
)

var sam2pairwiseCmd = &cobra.Command{
	Use:     "sam2pairwise [-m REF>ALT] [-l MARK] [-f] [-r]",
	Aliases: []string{"s2p"}, // Alias added
	Short:   "Convert SAM records from stdin into pairwise alignment format",
	Long: `Processes SAM records, parsing CIGAR and MD tags to generate pairwise alignments.

Highlighting Logic (with -m REF>ALT, e.g., -m C>T):
  - The specific REF>ALT mutation (C>T) is NOT highlighted.
  - The original REF base (C) when it's NOT mutated IS highlighted.
  - Other mutations involving the REF base (e.g., C>A, C>G) ARE highlighted.
  - All other mismatches not involving the specified REF base ARE highlighted.
  - Matches not involving the specified REF base are NOT highlighted.

Highlighting Logic (without -m):
  - All mismatches are highlighted with a colored background.
  - Matches are plain text.

Filtering Options:
  -f, --forward: Only process Read 1 Forward or Read 2 Reverse reads.
  -r, --reverse: Only process Read 1 Reverse or Read 2 Forward reads.
  (If neither -f nor -r is specified, all reads are processed).

Marking Mismatches:
  Optionally, use -m REF>ALT (e.g., -m C>T) and -l MARK (e.g., -l '.')
  to mark specific known mismatches with MARK instead of a space.

Long Intron Formatting (>20 Ns):
  Introns (N operations) longer than 20 bases are condensed in the output:
  Ref:   <darkgrey>NNNNN..[count]nt...NNNNN</darkgrey>  
  Query: <darkgrey>..... ..[count]nt... .....</darkgrey>  
  Marker:        [spaces matching width]`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(knownMutationMark) > 1 {
			fmt.Fprintln(os.Stderr, "Error: -l mark must be a single character.")
			os.Exit(1)
		}
		if knownMutation != "" && (len(knownMutation) != 3 || knownMutation[1] != '>') {
			fmt.Fprintln(os.Stderr, "Error: -m mutation format must be REF>ALT (e.g., C>T).")
			os.Exit(1)
		}
		if filterForward && filterReverse {
			fmt.Fprintln(os.Stderr, "Error: Cannot use -f and -r flags simultaneously.")
			os.Exit(1)
		}
		processSAMStdin()
	},
}

func init() {
	rootCmd.AddCommand(sam2pairwiseCmd)
	sam2pairwiseCmd.Flags().StringVarP(&knownMutation, "mutation", "m", "", "Known mutation to affect highlighting (e.g., C>T)")
	sam2pairwiseCmd.Flags().StringVarP(&knownMutationMark, "mark", "l", ".", "Single character to use for marking the known mutation")
	sam2pairwiseCmd.Flags().BoolVarP(&filterForward, "forward", "f", false, "Filter for Read 1 Forward or Read 2 Reverse")
	sam2pairwiseCmd.Flags().BoolVarP(&filterReverse, "reverse", "r", false, "Filter for Read 1 Reverse or Read 2 Forward")
}

func processSAMStdin() {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, syscall.SIGINT, syscall.SIGTERM)
	continueProcessing := true

	go func() {
		<-interruptChan
		continueProcessing = false
	}()

	scanner := bufio.NewScanner(os.Stdin)

	var knownRefBase, knownAltBase byte
	useKnownMutation := false
	if knownMutation != "" {
		knownRefBase = knownMutation[0]
		knownAltBase = knownMutation[2]
		useKnownMutation = true
	}
	markChar := '.'
	if len(knownMutationMark) > 0 {
		markChar = []rune(knownMutationMark)[0]
	}

	for continueProcessing && scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "@") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 11 {
			// Don't print errors for potentially truncated final lines if interrupted
			if continueProcessing {
				fmt.Fprintln(os.Stderr, "Skipping invalid SAM record (less than 11 fields):", line)
			}
			continue
		}

		readName := fields[0]
		flagStr := fields[1]
		refName := fields[2]
		pos := fields[3]
		cigar := fields[5]
		seq := fields[9]
		mdTagValue := ""

		// Apply filtering based on flags
		if filterForward || filterReverse {
			flag, err := strconv.Atoi(flagStr)
			if err != nil {
				if continueProcessing {
					fmt.Fprintf(os.Stderr, "Skipping invalid SAM record (invalid flag %s): %s\n", flagStr, line)
				}
				continue
			}

			isRead1 := (flag & 0x40) != 0
			isRead2 := (flag & 0x80) != 0
			isReverse := (flag & 0x10) != 0

			if filterForward {
				// Keep Read 1 Fwd (0x40 is set, 0x10 is not) OR Read 2 Rev (0x80 is set, 0x10 is set)
				if !((isRead1 && !isReverse) || (isRead2 && isReverse)) {
					continue // Skip if not matching forward filter criteria
				}
			} else if filterReverse {
				// Keep Read 1 Rev (0x40 is set, 0x10 is set) OR Read 2 Fwd (0x80 is set, 0x10 is not)
				if !((isRead1 && isReverse) || (isRead2 && !isReverse)) {
					continue // Skip if not matching reverse filter criteria
				}
			}
		}

		// Extract MD tag
		for _, field := range fields[11:] {
			if strings.HasPrefix(field, "MD:Z:") {
				mdTagValue = field[5:]
				break
			}
		}

		// Generate pairwise alignment
		refSeq, alignedSeq, markers, err := samToPairwise(seq, cigar, mdTagValue, useKnownMutation, knownRefBase, knownAltBase, markChar)
		if err != nil {
			if continueProcessing {
				fmt.Fprintf(os.Stderr, "Error processing read %s: %v\n", readName, err)
			}
			continue // Skip this read on error
		}

		// Print output if processing wasn't interrupted mid-generation
		if continueProcessing {
			// Print SAM info line (greyed out)
			tml.Printf("<darkgrey><italic>%s\t%s\t%s\t%s\t%s\t%s</italic></darkgrey>\n", readName, flagStr, refName, pos, cigar, mdTagValue)
			// Print alignment
			tml.Printf(alignedSeq + "\n") // Query sequence (potentially colored)
			fmt.Println(markers)          // Marker line
			tml.Printf(refSeq + "\n")     // Reference sequence (potentially colored)
			fmt.Println()                 // Blank line separator
		}
	}

	// Check for scanner errors, unless caused by interruption
	if err := scanner.Err(); err != nil {
		if continueProcessing { // Don't report scanner errors if we were interrupted
			fmt.Fprintln(os.Stderr, "Error reading standard input:", err)
		}
	}

	// Print exit message if interrupted
	if !continueProcessing {
		// Use tml for colored output to stderr as well
		tml.Fprintf(os.Stderr, "<yellow><bold>\nSignal received. Finishing current record and exiting.</bold></yellow>\n")
	}
}

// MDTagEntry holds parsed information from an MD tag component.
type MDTagEntry struct {
	Num     int    // Number of matching bases
	Changes string // Reference bases for mismatch or deletion
	IsDel   bool   // True if this entry represents a deletion (^)
}

// parseMDTag breaks down an MD:Z tag string into usable components.
func parseMDTag(mdTag string) ([]MDTagEntry, error) {
	var entries []MDTagEntry
	var numStr strings.Builder    // Collects digits for match counts
	var changeStr strings.Builder // Collects bases for mismatches/deletions
	state := "num"                // Initial state: expecting a number (match count)

	if mdTag == "" {
		return entries, nil // Empty MD tag is valid
	}

	// Helper function to add a match entry (number)
	addNumEntry := func() error {
		if numStr.Len() > 0 {
			num, err := strconv.Atoi(numStr.String())
			if err != nil {
				// This should be unlikely if the state machine is correct
				return fmt.Errorf("internal error: invalid number '%s' in MD tag parse", numStr.String())
			}
			entries = append(entries, MDTagEntry{Num: num})
			numStr.Reset()
		}
		return nil
	}

	// Helper function to add a deletion entry (^)
	addDelEntry := func() error {
		if changeStr.Len() > 0 {
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: true})
			changeStr.Reset()
			return nil
		}
		// Deletion '^' must be followed by deleted bases
		return fmt.Errorf("empty deletion sequence found after '^' in MD tag")
	}

	// Helper function to add a mismatch entry (single base)
	addMismatchEntry := func() error {
		if changeStr.Len() == 1 { // Mismatch should be a single reference base
			entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: false, Num: 0})
			changeStr.Reset()
			return nil
		}
		// This case indicates an error, like consecutive letters not after '^'
		return fmt.Errorf("invalid mismatch sequence '%s' (must be 1 base) in MD tag", changeStr.String())
	}

	// Iterate through the MD tag string character by character
	for i, char := range mdTag {
		isLastChar := (i == len(mdTag)-1)

		switch state {
		case "num": // Expecting digits for match count
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char) // Append digit
			} else { // Non-digit ends the number
				if err := addNumEntry(); err != nil { // Add the completed number entry
					return nil, err
				}
				// Transition to next state based on the non-digit character
				if char == '^' { // Start of deletion
					state = "del_start"
				} else if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') { // Start of mismatch
					changeStr.WriteRune(char)
					state = "change"
				} else { // Invalid character after a number
					return nil, fmt.Errorf("unexpected character '%c' after number in MD tag at position %d", char, i)
				}
			}
		case "change": // Expecting a mismatch base (already consumed one)
			// A mismatch is always a single base, so we must process it now
			if err := addMismatchEntry(); err != nil {
				return nil, err
			}
			// Transition to next state based on the current character
			if char >= '0' && char <= '9' { // Start of a new number (match count)
				numStr.WriteRune(char)
				state = "num"
			} else if char == '^' { // Start of a deletion
				state = "del_start"
			} else { // Invalid character after a mismatch base
				return nil, fmt.Errorf("unexpected character '%c' after mismatch base in MD tag at position %d", char, i)
			}
		case "del_start": // Just saw '^', expecting deleted base(s)
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char) // Start collecting deleted bases
				state = "del"
			} else { // Invalid character after '^'
				return nil, fmt.Errorf("expected base after deletion '^' in MD tag, got '%c' at position %d", char, i)
			}
		case "del": // Collecting deleted bases after '^'
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char) // Continue collecting
			} else { // Non-base character ends the deletion sequence
				if err := addDelEntry(); err != nil { // Add the completed deletion entry
					return nil, err
				}
				// Transition to next state based on the current character
				if char >= '0' && char <= '9' { // Start of a new number (match count)
					numStr.WriteRune(char)
					state = "num"
				} else if char == '^' { // Start of another deletion (unlikely but possible?)
					state = "del_start"
				} else { // Invalid character after deleted bases
					return nil, fmt.Errorf("unexpected character '%c' after deletion sequence in MD tag at position %d", char, i)
				}
			}
		}

		// After processing the character, handle the end of the string
		if isLastChar {
			switch state {
			case "num": // Ends with a number
				if err := addNumEntry(); err != nil {
					return nil, err
				}
			case "change": // Ends right after a mismatch base (invalid)
				// The mismatch should have been processed before this check
				return nil, fmt.Errorf("MD tag cannot end immediately after a mismatch base")
			case "del": // Ends with deleted bases
				if err := addDelEntry(); err != nil {
					return nil, err
				}
			case "del_start": // Ends with '^' (invalid)
				return nil, fmt.Errorf("MD tag cannot end with deletion start '^'")
			}
		}
	}
	return entries, nil
}

// samToPairwise converts SAM alignment info (SEQ, CIGAR, MD) into colored pairwise strings.
func samToPairwise(seq string, cigar string, mdTag string, useKnownMutation bool, knownRefBase byte, knownAltBase byte, markChar rune) (refSeqColored string, alignedSeqColored string, markers string, err error) {
	var refBuilder, alignedSeqBuilder, markerBuilder strings.Builder
	seqPos := 0 // Current position in the SEQ string

	// Parse CIGAR string into operations
	cigarOps, err := parseCigar(cigar)
	if err != nil {
		return "", "", "", fmt.Errorf("error parsing CIGAR '%s': %w", cigar, err)
	}

	// Parse MD tag if present and valid
	var mdEntries []MDTagEntry
	mdIndex := 0  // Current position in mdEntries
	mdSubPos := 0 // Current position within a multi-base MD entry (match count or deletion)
	hasMD := mdTag != ""
	mdParseErr := false // Flag if MD parsing failed
	if hasMD {
		mdEntries, err = parseMDTag(mdTag)
		if err != nil {
			// Warn about MD error but continue, treating ref bases as 'N' where needed
			fmt.Fprintf(os.Stderr, "Warning: Error parsing MD tag '%s', proceeding without full MD info: %v\n", mdTag, err)
			hasMD = false // Don't rely on MD info anymore
			mdParseErr = true
			mdEntries = nil // Clear potentially partial entries
		}
	}

	// Process each CIGAR operation
	for _, op := range cigarOps {
		length := op.Length
		opType := op.Op

		switch opType {
		case 'M', '=', 'X': // Match, Sequence Match, Sequence Mismatch
			for i := 0; i < length; i++ {
				// Check for premature end of sequence string
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR M/=/X asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				var refBase byte = 'N' // Default reference base if unknown
				isMismatch := false
				marker := '|' // Default marker for match

				// Determine reference base and mismatch status using MD tag if available
				if hasMD {
					// Skip potentially empty/placeholder MD entries
					for mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}

					// Check if we ran out of MD information
					if mdIndex >= len(mdEntries) {
						if !mdParseErr { // Only warn if MD parsing didn't already fail
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during M/=/X op%s. Assuming 'N' for reference base.\n",
								func() string { // Conditionally add MD tag to warning
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						hasMD = false // Stop using MD info
						refBase = 'N'
						isMismatch = true
					} else { // Still have MD info
						currentMdEntry := &mdEntries[mdIndex]
						if currentMdEntry.IsDel { // Deletion in MD during M/=/X CIGAR op? Inconsistent.
							return "", "", "", fmt.Errorf("MD tag indicates deletion (^) during CIGAR M/=/X at MD index %d (MD: %s)", mdIndex, mdTag)
						}

						if currentMdEntry.Num > 0 { // MD indicates a match
							refBase = readBase // Reference matches the read
							isMismatch = false
							mdSubPos++
							if mdSubPos == currentMdEntry.Num { // Consumed this block of matches
								mdIndex++
								mdSubPos = 0
							}
						} else { // MD indicates a mismatch
							if len(currentMdEntry.Changes) != 1 { // Malformed MD mismatch entry
								fmt.Fprintf(os.Stderr, "Warning: Invalid MD mismatch entry ('%s') in MD tag '%s'. Treating reference base as 'N'.\n", currentMdEntry.Changes, mdTag)
								refBase = 'N'
							} else {
								refBase = currentMdEntry.Changes[0] // Get ref base from MD
							}
							isMismatch = true
							mdIndex++ // Consumed this mismatch entry
							mdSubPos = 0
						}
					}
				} else { // No valid MD info available
					// If CIGAR is '=' it implies a match, otherwise assume mismatch or 'N'
					if opType == '=' {
						refBase = readBase
						isMismatch = false
					} else { // 'M' or 'X' without MD
						refBase = 'N'
						isMismatch = true
					}
				}

				// --- Determine Highlighting based on mismatch status and known mutation ---
				shouldHighlightRead := false
				shouldHighlightRef := false

				if isMismatch {
					marker = ' ' // Mismatches use space marker by default
					if useKnownMutation {
						if refBase == knownRefBase && readBase == knownAltBase {
							// This IS the known mutation: DON'T highlight, use special marker
							marker = markChar
						} else if refBase == knownRefBase {
							// Ref base matches known, but ALT is different: Highlight
							shouldHighlightRead = true
							shouldHighlightRef = true
						} else {
							// Mismatch doesn't involve the known ref base: Highlight
							shouldHighlightRead = true
							shouldHighlightRef = true
						}
					} else { // No known mutation specified: Highlight all mismatches
						shouldHighlightRead = true
						shouldHighlightRef = true
					}
				} else { // Is a Match
					marker = '|' // Matches use pipe marker
					if useKnownMutation && refBase == knownRefBase {
						// Match involves the known ref base (e.g. C matched C): Highlight
						shouldHighlightRead = true
						shouldHighlightRef = true
					} else {
						// Normal match: DON'T highlight
						shouldHighlightRead = false
						shouldHighlightRef = false
					}
				}
				// --- End Highlighting Logic ---

				// Append characters and colors to builders
				applyColor(&alignedSeqBuilder, readBase, shouldHighlightRead)
				applyColor(&refBuilder, refBase, shouldHighlightRef)
				markerBuilder.WriteRune(marker)

				seqPos++ // Move to the next base in the sequence
			}

		case 'I': // Insertion in read w.r.t. reference
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR I asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				applyColor(&alignedSeqBuilder, readBase, true) // Show read base, highlight
				applyColor(&refBuilder, '-', true)             // Show gap in ref, highlight
				markerBuilder.WriteByte(' ')                   // Space marker for difference
				seqPos++
			}
		case 'D': // Deletion in read w.r.t. reference
			if !hasMD { // Cannot determine the deleted reference bases
				if !mdParseErr { // Only warn if MD wasn't already known bad
					fmt.Fprintf(os.Stderr, "Warning: Deletion (D) in CIGAR but no valid MD tag. Representing deleted reference bases as 'N'.\n")
				}
				for i := 0; i < length; i++ {
					applyColor(&alignedSeqBuilder, '-', true) // Gap in read, highlight
					applyColor(&refBuilder, 'N', true)        // 'N' in ref, highlight
					markerBuilder.WriteByte(' ')              // Space marker
				}
			} else { // Use MD tag to find deleted reference bases
				deletedBasesFound := 0
				for deletedBasesFound < length {
					// Skip empty/placeholder MD entries
					for mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					if mdIndex >= len(mdEntries) { // Ran out of MD info
						if !mdParseErr {
							fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during D op%s. Representing remaining deleted bases as 'N'.\n",
								func() string {
									if mdTag != "" {
										return fmt.Sprintf(" (MD: %s)", mdTag)
									}
									return ""
								}())
						}
						// Fill remainder with 'N'
						for k := 0; k < length-deletedBasesFound; k++ {
							applyColor(&alignedSeqBuilder, '-', true)
							applyColor(&refBuilder, 'N', true)
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound = length // Mark as done
						hasMD = false              // Stop using MD
						break                      // Exit inner loop
					}

					currentMdEntry := &mdEntries[mdIndex]
					if currentMdEntry.IsDel { // MD confirms deletion
						delSeqLen := len(currentMdEntry.Changes)
						basesAvailableInMd := delSeqLen - mdSubPos
						basesNeededForCigar := length - deletedBasesFound
						basesToTake := min(basesNeededForCigar, basesAvailableInMd)

						for j := 0; j < basesToTake; j++ {
							delBase := currentMdEntry.Changes[mdSubPos+j]
							applyColor(&alignedSeqBuilder, '-', true) // Gap in read
							applyColor(&refBuilder, delBase, true)    // Deleted ref base
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound += basesToTake
						mdSubPos += basesToTake
						if mdSubPos == delSeqLen { // Consumed this MD deletion block
							mdIndex++
							mdSubPos = 0
						}
					} else { // MD indicates match/mismatch during CIGAR Deletion - Inconsistent!
						return "", "", "", fmt.Errorf("MD tag indicates match/mismatch (Num: %d, Changes: '%s') during CIGAR D op at MD index %d (MD: %s)", currentMdEntry.Num, currentMdEntry.Changes, mdIndex, mdTag)
					}
				} // End loop processing this CIGAR D op
			} // End else (hasMD)
		case 'N': // Intron (skipped region in reference)
			if length > minIntronCompressLength {
				// Condensed format: NNNNN..[count]nt...NNNNN
				middleStr := fmt.Sprintf("..%dnt..", length)
				displayWidth := condensedNSEdgeLength*2 + len(middleStr)

				// Reference string (Ns), greyed out
				refBuilder.WriteString("<darkgrey>")
				refBuilder.WriteString(strings.Repeat("N", condensedNSEdgeLength))
				refBuilder.WriteString(middleStr)
				refBuilder.WriteString(strings.Repeat("N", condensedNSEdgeLength))
				refBuilder.WriteString("</darkgrey>")

				// Aligned query string (dots), greyed out
				alignedSeqBuilder.WriteString("<darkgrey>")
				alignedSeqBuilder.WriteString(strings.Repeat(".", condensedNSEdgeLength))
				alignedSeqBuilder.WriteString(middleStr)
				alignedSeqBuilder.WriteString(strings.Repeat(".", condensedNSEdgeLength))
				alignedSeqBuilder.WriteString("</darkgrey>")

				// Markers (spaces matching width)
				markerBuilder.WriteString(strings.Repeat(" ", displayWidth))

			} else { // Shorter intron, show individual Ns/dots
				for i := 0; i < length; i++ {
					applyColor(&alignedSeqBuilder, '.', false) // Query: dot, no highlight
					applyColor(&refBuilder, 'N', false)        // Ref: N, no highlight
					markerBuilder.WriteByte(' ')               // Space marker
				}
			}
		case 'S': // Soft clipping (bases in SEQ but not aligned)
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR S asks for base %d but sequence length is %d", seqPos+1, len(seq))
				}
				readBase := seq[seqPos]
				applyColor(&alignedSeqBuilder, readBase, true) // Show read base, highlight
				applyColor(&refBuilder, '.', false)            // Use '.' in ref, no highlight
				markerBuilder.WriteByte(' ')                   // Space marker
				seqPos++
			}
		case 'H': // Hard clipping (bases NOT in SEQ)
			// No output in pairwise alignment, does not consume sequence
			continue
		case 'P': // Padding (silent deletion from padded reference)
			for i := 0; i < length; i++ {
				applyColor(&alignedSeqBuilder, '*', true) // Show '*' in read, highlight
				applyColor(&refBuilder, '*', true)        // Show '*' in ref, highlight
				markerBuilder.WriteByte(' ')              // Space marker
			}
		default: // Unsupported CIGAR operation
			return "", "", "", fmt.Errorf("unsupported CIGAR operation: %c", opType)
		}
	}

	// Final consistency check: Did CIGAR consume the expected number of sequence bases?
	expectedSeqLenConsumed := 0
	for _, op := range cigarOps {
		if strings.ContainsRune("MIS=X", op.Op) { // These ops consume SEQ bases
			expectedSeqLenConsumed += op.Length
		}
	}
	// Check if the actual consumed sequence position matches the expectation
	if seqPos != expectedSeqLenConsumed && len(seq) > 0 {
		// This often indicates an issue with the CIGAR/SEQ in the SAM record itself
		fmt.Fprintf(os.Stderr, "Warning: CIGAR/SEQ inconsistency for read. CIGAR (M,I,S,=,X) expected %d bases, but %d processed from sequence length %d.\n", expectedSeqLenConsumed, seqPos, len(seq))
	}
	// Optionally, check if seqPos < len(seq) which might mean trailing soft clips weren't fully represented in CIGAR (can be normal)

	return refBuilder.String(), alignedSeqBuilder.String(), markerBuilder.String(), nil
}

// applyColor wraps a base character with tml color tags based on the base and highlight flag.
func applyColor(builder *strings.Builder, base byte, shouldHighlight bool) {
	color := "" // tml color name (e.g., "red", "black")

	// Determine color only if highlighting might be needed
	if shouldHighlight {
		switch base {
		case 'A', 'a':
			color = "red"
		case 'T', 't':
			color = "green"
		case 'G', 'g':
			color = "yellow"
		case 'C', 'c':
			color = "blue"
		case '-':
			color = "black" // Gap highlight color
		case '*':
			color = "magenta" // Padding highlight color
			// N, ., etc., don't get background colors even if conceptually "different"
		}
	}

	// Apply formatting based on base type and whether highlighting is active
	switch base {
	case 'A', 'a', 'T', 't', 'G', 'g', 'C', 'c':
		if shouldHighlight && color != "" {
			builder.WriteString("<bg-" + color + ">") // Open tag
			builder.WriteByte(base)
			builder.WriteString("</bg-" + color + ">") // Close tag
		} else {
			builder.WriteByte(base) // No background highlight
		}
	case '-': // Gap
		if shouldHighlight && color == "black" { // Highlight gaps with black background
			builder.WriteString("<bg-black>")
			builder.WriteByte(base)
			builder.WriteString("</bg-black>")
		} else {
			builder.WriteByte(base) // Just the gap character
		}
	case '*': // Padding
		if shouldHighlight && color == "magenta" { // Highlight padding with magenta background
			builder.WriteString("<bg-magenta>")
			builder.WriteByte(base)
			builder.WriteString("</bg-magenta>")
		} else {
			builder.WriteByte(base)
		}
	case 'N', 'n': // Ambiguous or Intron Reference
		// Always dark grey text, never background highlight
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
	case '.': // Intron Query, or Ref placeholder for Soft/Hard clip
		// Always dark grey text, never background highlight
		builder.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
	default: // Any other character
		builder.WriteByte(base) // Print as is
	}
}

// CigarOp represents a single CIGAR operation (Length and Type).
type CigarOp struct {
	Length int
	Op     rune
}

// parseCigar converts a CIGAR string into a slice of CigarOp structs.
func parseCigar(cigar string) ([]CigarOp, error) {
	// Handle empty or '*' CIGAR string (common for unaligned reads)
	if cigar == "*" || cigar == "" {
		return []CigarOp{}, nil
	}

	var ops []CigarOp
	var lengthStr strings.Builder // Temporarily stores digits for the length

	for i, char := range cigar {
		if char >= '0' && char <= '9' { // If it's a digit, append to length string
			lengthStr.WriteRune(char)
		} else if strings.ContainsRune("MIDNSHP=X", char) { // If it's a valid CIGAR op type
			if lengthStr.Len() == 0 { // Operation type must be preceded by a length
				return nil, fmt.Errorf("CIGAR operation '%c' at index %d has no preceding length", char, i)
			}
			length, err := strconv.Atoi(lengthStr.String())
			if err != nil { // Should be rare if digits were checked, but good practice
				return nil, fmt.Errorf("invalid CIGAR length format '%s' before op '%c': %w", lengthStr.String(), char, err)
			}
			if length <= 0 { // Length must be positive according to SAM spec
				return nil, fmt.Errorf("invalid non-positive CIGAR length %d for operation '%c'", length, char)
			}
			// Add the parsed operation to the list
			ops = append(ops, CigarOp{Length: length, Op: char})
			lengthStr.Reset() // Reset length builder for the next operation
		} else { // Invalid character found in CIGAR string
			return nil, fmt.Errorf("invalid character '%c' in CIGAR string at index %d", char, i)
		}
	}

	// After loop, check if there are leftover digits without an operation type
	if lengthStr.Len() > 0 {
		return nil, fmt.Errorf("CIGAR string ends with an incomplete operation (number '%s' without type)", lengthStr.String())
	}
	return ops, nil
}

// min returns the smaller of two integers. Used for MD tag deletion logic.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
