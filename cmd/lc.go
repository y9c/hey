package cmd

import (
	"github.com/spf13/cobra"
	"io"
	"os"
	"runtime"
	"sync"
)

var (
	lcCmd = &cobra.Command{
		Use:   "lc",
		Short: "Quicker way to count line number",
		Long:  `Better than linux build-in wc and gzip format will be supported`,
		Run: func(cmd *cobra.Command, args []string) {
			countLines(args[0])
		},
	}
)

func init() {
	rootCmd.AddCommand(lcCmd)
}

func countLines(filePath string) {

	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	fileReader := &FileReader{
		File: file,
	}
	counts := make(chan Count)

	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		go FileReaderCounter(fileReader, counts)
	}

	totalCount := Count{}
	for i := 0; i < numWorkers; i++ {
		count := <-counts
		totalCount.LineCount += count.LineCount
	}
	close(counts)

	println(file.Name(), totalCount.LineCount)
}

type FileReader struct {
	File  *os.File
	mutex sync.Mutex
}

func (fileReader *FileReader) ReadChunk(buffer []byte) (Chunk, error) {
	fileReader.mutex.Lock()
	defer fileReader.mutex.Unlock()

	bytes, err := fileReader.File.Read(buffer)
	if err != nil {
		return Chunk{}, err
	}

	chunk := Chunk{buffer[:bytes]}

	return chunk, nil
}

func FileReaderCounter(fileReader *FileReader, counts chan Count) {
	const bufferSize = 16 * 1024
	buffer := make([]byte, bufferSize)

	totalCount := Count{}

	for {
		chunk, err := fileReader.ReadChunk(buffer)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				panic(err)
			}
		}
		count := GetCount(chunk)
		totalCount.LineCount += count.LineCount
	}

	counts <- totalCount
}

type Chunk struct {
	Buffer []byte
}

type Count struct {
	LineCount int
}

func GetCount(chunk Chunk) Count {
	count := Count{}

	for _, b := range chunk.Buffer {
		switch b {
		case '\n':
			count.LineCount++
		default:
		}
	}

	return count
}
