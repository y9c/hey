package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/liamg/tml" // Import tml for coloring
	"github.com/spf13/cobra"
)

// --- Added flag variables ---
var (
	knownMutation     string
	knownMutationMark string
)

// sam2pairwiseCmd processes SAM records from stdin into pairwise alignments
var sam2pairwiseCmd = &cobra.Command{
	Use:   "sam2pairwise [-m REF>ALT] [-l MARK]",
	Short: "Convert SAM records from stdin into pairwise alignment format",
	Long: `Processes SAM records, parsing CIGAR and MD tags to generate pairwise alignments.
Aligned bases (A/T/G/C) are shown with background colors for readability.
Optionally, use -m REF>ALT (e.g., -m C>T) and -l MARK (e.g., -l '*')
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
		processSAMStdin()
	},
}

func init() {
	rootCmd.AddCommand(sam2pairwiseCmd)
	// --- Define flags ---
	sam2pairwiseCmd.Flags().StringVarP(&knownMutation, "mutation", "m", "", "Known mutation to mark (e.g., C>T)")
	// Default mark is '.' as in the C++ version
	sam2pairwiseCmd.Flags().StringVarP(&knownMutationMark, "mark", "l", ".", "Single character to use for marking the known mutation")
}

// colorizeAlignmentString adds TML background color tags to a sequence string
func colorizeAlignmentString(sequence string) string {
	var coloredSequence strings.Builder
	for _, base := range sequence {
		switch base {
		case 'A', 'a':
			coloredSequence.WriteString(tml.Sprintf("<bg-red>%c</bg-red>", base))
		case 'T', 't':
			coloredSequence.WriteString(tml.Sprintf("<bg-green>%c</bg-green>", base))
		case 'G', 'g':
			coloredSequence.WriteString(tml.Sprintf("<bg-yellow>%c</bg-yellow>", base))
		case 'C', 'c':
			coloredSequence.WriteString(tml.Sprintf("<bg-blue>%c</bg-blue>", base))
		case '-': // Gap - Use black background
			coloredSequence.WriteString(tml.Sprintf("<bg-black>%c</bg-black>", base))
		case 'N', 'n': // Ambiguous / Clipped Ref / Skipped Ref - Use dark grey foreground
			coloredSequence.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
		case '.': // Skipped Read - Use dark grey foreground
			coloredSequence.WriteString(tml.Sprintf("<darkgrey>%c</darkgrey>", base))
		case '*': // Padding - Use magenta background
			coloredSequence.WriteString(tml.Sprintf("<bg-magenta>%c</bg-magenta>", base))
		default: // Other characters (just in case)
			coloredSequence.WriteRune(base)
		}
	}
	return coloredSequence.String()
}

// processSAMStdin reads SAM records from stdin and converts each record into pairwise alignment
func processSAMStdin() {
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

	for scanner.Scan() {
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
		flag := fields[1]
		refName := fields[2] // Reference sequence name
		pos := fields[3]     // 1-based leftmost mapping POSition
		cigar := fields[5]   // CIGAR string
		seq := fields[9]     // Segment SEQuence
		mdTagValue := ""     // Initialize MD tag value

		// Find and extract the MD tag *value*
		for _, field := range fields[11:] {
			if strings.HasPrefix(field, "MD:Z:") {
				mdTagValue = field[5:]
				break
			}
		}

		// --- Updated call to pass flag info ---
		refSeq, alignedSeq, markers, err := samToPairwise(seq, cigar, mdTagValue, useKnownMutation, knownRefBase, knownAltBase, markChar)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing read %s: %v\n", readName, err)
			continue // Skip this read and proceed to the next
		}

		// --- Header Line Output (User Requested Format) ---
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", readName, flag, refName, pos, cigar, mdTagValue)
		// --- End of Header Line ---

		// Print colored alignment strings
		tml.Println(colorizeAlignmentString(alignedSeq))
		fmt.Println(markers) // Markers remain uncolored
		tml.Println(colorizeAlignmentString(refSeq))
		fmt.Println() // Blank line separator
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading standard input:", err)
	}
}

// extractMDTag finds and returns the MD tag value from the SAM optional fields
// Kept for potential other uses, but not strictly needed by processSAMStdin anymore.
func extractMDTag(optionalFields []string) string {
	for _, field := range optionalFields {
		if strings.HasPrefix(field, "MD:Z:") {
			return field[5:]
		}
	}
	return ""
}

// MDTagEntry represents an entry in the parsed MD tag
type MDTagEntry struct {
	Num     int    // Number of matching bases
	Changes string // Mismatches or deletions (bases from reference)
	IsDel   bool   // Is this a deletion?
}

// parseMDTag parses the MD tag into a structured list of MDTagEntry objects
func parseMDTag(mdTag string) ([]MDTagEntry, error) {
	var entries []MDTagEntry
	var numStr strings.Builder
	var changeStr strings.Builder
	state := "num" // States: num, change, del_start, del

	for _, char := range mdTag {
		switch state {
		case "num":
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
			} else {
				if numStr.Len() > 0 {
					num, err := strconv.Atoi(numStr.String())
					if err != nil {
						return nil, fmt.Errorf("invalid number '%s' in MD tag", numStr.String())
					}
					entries = append(entries, MDTagEntry{Num: num})
					numStr.Reset()
				}
				if char == '^' {
					state = "del_start"
				} else if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
					changeStr.WriteRune(char)
					state = "change"
				} else {
					return nil, fmt.Errorf("unexpected character '%c' after number in MD tag", char)
				}
			}
		case "change":
			// A change (mismatch) is always a single base in the MD tag format.
			entries = append(entries, MDTagEntry{Changes: changeStr.String()})
			changeStr.Reset()
			if char >= '0' && char <= '9' {
				numStr.WriteRune(char)
				state = "num"
			} else if char == '^' {
				state = "del_start"
			} else {
				// After a mismatch base, expect a number or deletion start
				return nil, fmt.Errorf("unexpected character '%c' after mismatch base in MD tag", char)
			}
		case "del_start":
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
				state = "del"
			} else {
				return nil, fmt.Errorf("expected base after deletion '^' in MD tag, got '%c'", char)
			}
		case "del":
			if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
				changeStr.WriteRune(char)
				// Stay in del state to accumulate deleted bases
			} else {
				// End of deletion sequence
				entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: true})
				changeStr.Reset()
				if char >= '0' && char <= '9' {
					numStr.WriteRune(char)
					state = "num"
				} else if char == '^' {
					// Another deletion starts immediately? (e.g., ^A^T)
					state = "del_start"
				} else {
					// Anything else is invalid after deleted sequence
					return nil, fmt.Errorf("unexpected character '%c' after deletion sequence in MD tag", char)
				}
			}
		}
	}

	// Handle trailing state
	switch state {
	case "num":
		if numStr.Len() > 0 {
			num, err := strconv.Atoi(numStr.String())
			if err != nil {
				return nil, fmt.Errorf("invalid trailing number '%s' in MD tag", numStr.String())
			}
			entries = append(entries, MDTagEntry{Num: num})
		}
	case "change":
		// A change must be followed by num or ^
		return nil, fmt.Errorf("MD tag cannot end after a mismatch base")
	case "del":
		entries = append(entries, MDTagEntry{Changes: changeStr.String(), IsDel: true})
	case "del_start":
		return nil, fmt.Errorf("MD tag cannot end with deletion start '^'")

	}

	return entries, nil
}

// --- Updated function signature to accept flag info ---
func samToPairwise(seq string, cigar string, mdTag string, useKnownMutation bool, knownRefBase byte, knownAltBase byte, markChar rune) (refSeq string, alignedSeq string, markers string, err error) {
	var refBuilder, alignedSeqBuilder, markerBuilder strings.Builder
	seqPos := 0 // Current position in the input sequence string

	// CIGAR parsing
	cigarOps, err := parseCigar(cigar)
	if err != nil {
		return "", "", "", fmt.Errorf("error parsing CIGAR '%s': %w", cigar, err)
	}

	// MD tag parsing (only if MD tag exists)
	var mdEntries []MDTagEntry
	mdIndex := 0 // Current position in mdEntries
	mdSubPos := 0 // Position within the current MD entry (e.g., within a number or deletion string)
	hasMD := mdTag != ""
	if hasMD {
		mdEntries, err = parseMDTag(mdTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error parsing MD tag '%s', proceeding without it: %v\n", mdTag, err)
			hasMD = false
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
					// --- Logic to skip potential {Num: 0} entries before processing ---
					for hasMD && mdIndex < len(mdEntries) && mdEntries[mdIndex].Num == 0 && len(mdEntries[mdIndex].Changes) == 0 && !mdEntries[mdIndex].IsDel {
						mdIndex++
					}
					// --- End Skip Logic ---

					// Check if we still have valid MD entry after potentially skipping Num:0
					if mdIndex >= len(mdEntries) {
						fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during M/=/X op%s. Assuming 'N' for reference base.\n",
							func() string { if mdTag != "" { return fmt.Sprintf(" (MD: %s)", mdTag); } return "" }()) // Show MD if available
						hasMD = false // Stop trusting MD tag
					}

					if hasMD {
						currentMdEntry := &mdEntries[mdIndex]
						if currentMdEntry.IsDel {
							// This is an inconsistency between CIGAR and MD tag
							return "", "", "", fmt.Errorf("MD tag indicates deletion (^) during CIGAR match (M/=/X) operation at MD index %d (MD: %s)", mdIndex, mdTag)
						}

						if currentMdEntry.Num > 0 { // --- Case 1: Matching block in MD ---
							refBase = readBase
							isMismatch = false // Explicitly ensure it's not marked mismatch
							mdSubPos++
							if mdSubPos == currentMdEntry.Num {
								mdIndex++
								mdSubPos = 0
							}
						} else { // --- Case 2: Mismatch block {Changes: "X"} --- (Num must be 0 here)
							if len(currentMdEntry.Changes) != 1 {
								// This case handles the previously reported error if parseMDTag failed, but Num:0 fix should prevent it mostly
								fmt.Fprintf(os.Stderr, "Warning: Invalid MD mismatch entry detected (expected 1 char, got '%s') in MD tag '%s'. Treating reference base as 'N'.\n", currentMdEntry.Changes, mdTag)
								refBase = 'N' // Treat as unknown reference base
								isMismatch = true
							} else {
								refBase = currentMdEntry.Changes[0] // Base from reference
								isMismatch = true
							}
							// Mismatch entry is consumed entirely in one step
							mdIndex++
							mdSubPos = 0
						}
					} else { // No valid MD info left (e.g., reached end prematurely)
						refBase = 'N' // Indicate unknown reference base
						isMismatch = true
					}

				} else if opType == 'X' || (opType == 'M' && !hasMD) {
					refBase = 'N' // Indicate unknown reference base
					isMismatch = true
				}
				// If CIGAR is '=', it's a match, refBase is already readBase.

				alignedSeqBuilder.WriteByte(readBase)
				refBuilder.WriteByte(refBase)

				if isMismatch {
					// Check if it's the known mutation
					if useKnownMutation && refBase == knownRefBase && readBase == knownAltBase {
						markerBuilder.WriteRune(markChar)
					} else {
						markerBuilder.WriteByte(' ') // Space for other mismatches
					}
				} else {
					markerBuilder.WriteByte('|') // Match
				}
				seqPos++
			} // end inner loop for length

		case 'I': // Insertion to the reference
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (I op)")
				}
				alignedSeqBuilder.WriteByte(seq[seqPos])
				refBuilder.WriteByte('-')
				markerBuilder.WriteByte(' ')
				seqPos++
			}
		case 'D': // Deletion from the reference
			if !hasMD {
				fmt.Fprintf(os.Stderr, "Warning: Deletion (D) in CIGAR but no MD tag found. Representing deleted reference bases as 'N'.\n")
				for range length { // Modernized loop
					alignedSeqBuilder.WriteByte('-')
					refBuilder.WriteByte('N') // Unknown deleted base
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
						fmt.Fprintf(os.Stderr, "Warning: Reached end of MD tag prematurely during D op%s. Representing remaining deleted bases as 'N'.\n",
							func() string { if mdTag != "" { return fmt.Sprintf(" (MD: %s)", mdTag); } return "" }())
						for range length - deletedBasesFound { // Modernized loop
							alignedSeqBuilder.WriteByte('-')
							refBuilder.WriteByte('N')
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound = length // Break outer loop
						hasMD = false             // Stop trusting MD tag
						break                     // Break inner MD processing loop
					}

					currentMdEntry := &mdEntries[mdIndex]

					// Now check IsDel *after* skipping Num:0
					if currentMdEntry.IsDel {
						delSeqLen := len(currentMdEntry.Changes)
						basesAvailable := delSeqLen - mdSubPos
						basesNeeded := length - deletedBasesFound
						basesToTake := min(basesNeeded, basesAvailable)

						for i := range basesToTake { // Modernized loop
							alignedSeqBuilder.WriteByte('-')
							refBuilder.WriteByte(currentMdEntry.Changes[mdSubPos+i]) // Index i is used here
							markerBuilder.WriteByte(' ')
						}
						deletedBasesFound += basesToTake
						mdSubPos += basesToTake

						if mdSubPos == delSeqLen {
							mdIndex++
							mdSubPos = 0
						}
					} else {
						// MD tag indicates match/mismatch during CIGAR deletion (D) operation
						// This error is likely correct, indicating inconsistent SAM input.
						return "", "", "", fmt.Errorf("MD tag indicates match/mismatch (Num: %d, Changes: '%s') during CIGAR deletion (D) operation at MD index %d (MD: %s)", currentMdEntry.Num, currentMdEntry.Changes, mdIndex, mdTag)
					}
				} // end while deletedBasesFound < length
			}
		case 'N': // Skipped region from the reference (e.g., intron)
			for range length { // Modernized loop
				alignedSeqBuilder.WriteByte('.') // Use '.' for skipped region in read alignment visualization
				refBuilder.WriteByte('N')
				markerBuilder.WriteByte(' ')
				// Note: seqPos does NOT advance for N operation
			}
		case 'S': // Soft clipping (clipped sequences present in SEQ)
			for range length { // Modernized loop
				if seqPos >= len(seq) {
					return "", "", "", fmt.Errorf("CIGAR asks for base beyond sequence length (S op)")
				}
				alignedSeqBuilder.WriteByte(seq[seqPos])
				refBuilder.WriteByte('N') // Reference doesn't align here
				markerBuilder.WriteByte(' ')
				seqPos++
			}

		case 'H': // Hard clipping (clipped sequences NOT in SEQ)
			continue // Do nothing, seqPos does not advance
		case 'P': // Padding (silent deletion from padded reference)
			for range length { // Modernized loop
				alignedSeqBuilder.WriteByte('*')
				refBuilder.WriteByte('*')
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
		if seqPos < len(seq) {
			fmt.Fprintf(os.Stderr, "Warning: CIGAR operations consumed %d bases, but sequence length is %d. Result might be truncated or CIGAR/SEQ inconsistent.\n", seqPos, len(seq))
		}
	}

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
				return nil, fmt.Errorf("CIGAR operation '%c' has no preceding length", char)
			}
			length, err := strconv.Atoi(lengthStr.String())
			if err != nil || length <= 0 {
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

// Go 1.21 min function (can remove if using earlier Go version)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
