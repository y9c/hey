# hey

## Installation

Pre-built binaries are available at [https://github.com/y9c/hey/releases/tag/latest](https://github.com/y9c/hey/releases/tag/latest).

To install or upgrade `hey`, use the provided `install.sh` script:

```bash
curl -sfL https://raw.githubusercontent.com/y9c/hey/main/install.sh | sh
```

This script will automatically detect your system, download the appropriate binary, and install it to a suitable location in your PATH.

## Commands showcase

- **open**: Open file in server with browser.
  ![](./docs/preview_open.png)
- **tsv**: Preview tsv file in a pretty way.
  ![](./docs/preview_tsv.png)
- **colname**: Transpose and format table, showing column names and initial data rows.
  ![](./docs/preview_colname.png)
- **fastq**: Colorize and visualize FASTQ files, including quality scores and adapter detection.
  ![](./docs/preview_fastq.png)
- **sam (sam2pairwise)**: Convert SAM records into pairwise alignment format with highlighting.
  ![](./docs/preview_sam2pairwise.png)
- **stats**: Concatenate and transpose columns from files into a matrix.
- **wc**: Count lines, words, and characters in files (gzip supported).
- **rname**: Identify instrument, flow cell type, and lane from FASTQ read names.
- **rc**: Compute the reverse complement of DNA sequences.
