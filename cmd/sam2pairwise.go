package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// sam2pairwiseCmd processes SAM records from stdin into pairwise alignments
var sam2pairwiseCmd = &cobra.Command{
	Use:   "sam2pairwise",
	Short: "Convert SAM records from stdin into pairwise alignment format",
	Long:  `Processes SAM records, parsing CIGAR and MD tags to generate pairwise alignments.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Read input from stdin
		processSAMStdin()
	},
}

func init() {
	rootCmd.AddCommand(sam2pairwiseCmd)
}

// processSAMStdin reads SAM records from stdin and converts each record into pairwise alignment
func processSAMStdin() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "@") {
			// Skip header lines
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 11 {
			fmt.Println("Invalid SAM record")
			continue
		}

		// Parse the necessary fields from the SAM record
		readName := fields[0]
		flag := fields[1]
		seq := fields[9]   // Sequence
		cigar := fields[5] // CIGAR string
		pos := fields[3]
		mdTag := extractMDTag(fields[11:]) // MD tag is typically found at the 12th field onwards

		// Convert SAM record to pairwise alignment
		ref, alignedSeq, markers := samToPairwise(seq, cigar, mdTag)

		// Output the alignment in the desired format, replacing fields[7] with mdTag
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", readName, flag, fields[2], pos, cigar, mdTag)
		fmt.Println(alignedSeq) // Output the query sequence in full, without clipping
		fmt.Println(markers)
		fmt.Println(ref) // Reference sequence with Ns for soft clips
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading input:", err)
	}
}

// extractMDTag finds and returns the MD tag from the SAM optional fields
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
	Changes string // Mismatches or deletions
	IsDel   bool   // Is this a deletion?
}

// parseMDTag parses the MD tag into a structured list of MDTagEntry objects
func parseMDTag(mdTag string) []MDTagEntry {
	var entries []MDTagEntry
	var numStr strings.Builder
	isDel := false

	for i := 0; i < len(mdTag); i++ {
		char := mdTag[i]
		if char >= '0' && char <= '9' {
			// This is part of a number (match length)
			numStr.WriteByte(char)
		} else {
			// Non-number: end of a match length, or start of a deletion
			if numStr.Len() > 0 {
				// Add the match entry
				num, _ := strconv.Atoi(numStr.String())
				entries = append(entries, MDTagEntry{
					Num:   num,
					IsDel: false,
				})
				numStr.Reset()
			}

			if char == '^' {
				// Start of a deletion
				isDel = true
			} else if isDel {
				// Deletion sequence
				if len(entries) > 0 && entries[len(entries)-1].IsDel {
					entries[len(entries)-1].Changes += string(char)
				} else {
					entries = append(entries, MDTagEntry{
						Num:     0,
						Changes: string(char),
						IsDel:   true,
					})
				}
			} else {
				// Mismatch (single base change)
				entries = append(entries, MDTagEntry{
					Num:     0,
					Changes: string(char),
					IsDel:   false,
				})
			}
		}
	}

	// Handle any remaining number at the end
	if numStr.Len() > 0 {
		num, _ := strconv.Atoi(numStr.String())
		entries = append(entries, MDTagEntry{
			Num:   num,
			IsDel: false,
		})
	}

	return entries
}

// samToPairwise converts the SAM record to a pairwise alignment using CIGAR and MD tags
func samToPairwise(seq string, cigar string, mdTag string) (string, string, string) {
	var refBuilder, alignedSeqBuilder, markerBuilder strings.Builder
	seqPos := 0     // Position in the query sequence
	refPos := 0     // Position in the reference sequence
	clipLength := 0 // Track the length of the soft clip

	// Parse MD tag to modify the reference sequence based on mismatches
	mdEntries := parseMDTag(mdTag)

	// Process CIGAR string to handle insertions, deletions, soft clips, and matches
	cigarOps := parseCigar(cigar)

	mdIndex := 0 // Track which part of the MD tag we are at

	for _, op := range cigarOps {
		length := op.Length
		switch op.Op {
		case 'M': // Match or mismatch
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					break
				}

				refBase := seq[refPos+clipLength] // Adjust reference position by clip length for match
				alignedBase := seq[seqPos]

				// Update reference and sequence
				refBuilder.WriteByte(refBase)
				alignedSeqBuilder.WriteByte(alignedBase)

				// Update markers: Use | for exact matches, and . for mismatches
				if refBase == alignedBase {
					markerBuilder.WriteByte('|') // Match
				} else {
					markerBuilder.WriteByte('.') // Mismatch
				}
				seqPos++
				refPos++
			}
		case 'I': // Insertion in the read
			for i := 0; i < length; i++ {
				if seqPos >= len(seq) {
					break
				}
				refBuilder.WriteByte('-') // Gap in the reference (adjusted by clip length)
				alignedSeqBuilder.WriteByte(seq[seqPos])
				markerBuilder.WriteByte(' ') // No marker for insertion
				seqPos++
        refPos++
			}
		case 'D': // Deletion from the reference
			// For deletions, extract the original sequence from MD tag and add it to the reference
			for j := 0; j < length; j++ {
				// Find the deleted sequence from MD tag
				for mdIndex < len(mdEntries) {
					entry := mdEntries[mdIndex]
					if entry.IsDel {
						// Add the deleted bases from MD tag to the reference
						for _, delBase := range entry.Changes {
							refBuilder.WriteByte(byte(delBase)) // Insert deletion base into reference
							alignedSeqBuilder.WriteByte('-')    // Gap in aligned sequence
							markerBuilder.WriteByte(' ')        // No marker for deletion
						}
						mdIndex++ // Move to the next part of the MD tag
						break
					} else {
						mdIndex++
					}
				}
			}
		case 'S': // Soft clipping (present in the read but not aligned to the reference)
			clipLength = length // Store the clip length for future shifts in the reference
			// Add 'N's in the reference sequence for the soft-clipped bases
			for i := 0; i < clipLength; i++ {
				refBuilder.WriteByte('N')                // Add 'N' to the reference for soft-clipped bases
				alignedSeqBuilder.WriteByte(seq[seqPos]) // Keep the soft-clipped query sequence intact
				markerBuilder.WriteByte(' ')             // No marker for soft clips
				seqPos++
			}
		}
	}

	// Return the reference sequence (with `N`s), the aligned query (without soft clip removal), and the markers
	return refBuilder.String(), alignedSeqBuilder.String(), markerBuilder.String()
}

// parseCigar parses a CIGAR string and returns a slice of CigarOps
func parseCigar(cigar string) []CigarOp {
	var ops []CigarOp
	var lengthStr strings.Builder

	for _, char := range cigar {
		if char >= '0' && char <= '9' {
			lengthStr.WriteRune(char)
		} else {
			length, _ := strconv.Atoi(lengthStr.String())
			ops = append(ops, CigarOp{
				Length: length,
				Op:     char,
			})
			lengthStr.Reset()
		}
	}

	return ops
}

// CigarOp represents a parsed CIGAR operation
type CigarOp struct {
	Length int
	Op     rune
}
