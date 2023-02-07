package documents

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/blevesearch/bleve"
	"github.com/blugelabs/bluge"
	"github.com/seanhagen/fulltext"
	"github.com/stretchr/testify/require"
)

func BenchmarkIndexingWithBluge(b *testing.B) {
	tests := []string{
		"large",
		"medium",
		"small",
	}

	for _, x := range tests {
		tt := x
		confPath := fmt.Sprintf("./testdata/%s-index.bluge", tt)
		path := fmt.Sprintf("./testdata/%s_files", tt)

		b.Run(fmt.Sprintf("type %s", tt), func(b *testing.B) {
			benchmarkFilesWithBluge(b, confPath, path)
		})
	}
}

func BenchmarkIndexingFilesWithFulltext(b *testing.B) {
	tests := []string{
		"large",
		"medium",
		"small",
	}

	for _, x := range tests {
		tt := x
		path := fmt.Sprintf("./testdata/%s_files", tt)
		b.Run(fmt.Sprintf("type %s", tt), func(b *testing.B) {
			benchmarkFilesWithFulltext(b, path)
		})
	}
}

func BenchmarkIndexingFilesWithBleve(b *testing.B) {
	tests := []string{
		"large",
		"medium",
		"small",
	}

	for _, x := range tests {
		tt := x
		path := fmt.Sprintf("./testdata/%s_files", tt)
		b.Run(fmt.Sprintf("type %s", tt), func(b *testing.B) {
			benchmarkWithBleve(b, path)
		})
	}
}

// benchmarkFilesWithBluge ...
func benchmarkFilesWithBluge(b *testing.B, confPath, path string) {
	b.Helper()

	config := bluge.DefaultConfig(confPath)
	writer, err := bluge.OpenWriter(config)
	require.NoError(b, err)
	defer writer.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dirfs := os.DirFS(path)

		fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, err error) error {
			if path == "." || d.IsDir() {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("unable to get info on %q; %w", path, err)
			}

			file, err := dirfs.Open(path)
			if err != nil {
				return fmt.Errorf("unable to open file %q; %w", path, err)
			}

			buf := bytes.NewBuffer(nil)
			_, err = io.Copy(buf, file)

			b.StartTimer()
			doc := bluge.NewDocument(path).
				AddField(bluge.NewTextField("name", path)).
				AddField(bluge.NewNumericField("size", float64(info.Size()))).
				AddField(bluge.NewTextField("contents", buf.String()))
			defer b.StopTimer()

			return writer.Update(doc.ID(), doc)
		})
	}
}

// benchmarkFilesWithFulltext ...
func benchmarkFilesWithFulltext(b *testing.B, path string) {
	idx, err := fulltext.NewIndexer("")
	require.NoError(b, err)
	defer idx.Close()

	idx.StopWordCheck = fulltext.EnglishStopWordChecker

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dirfs := os.DirFS(path)

		fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, err error) error {
			if path == "." || d.IsDir() {
				return nil
			}

			file, err := dirfs.Open(path)
			if err != nil {
				return fmt.Errorf("unable to open file %q; %w", path, err)
			}

			buf := bytes.NewBuffer(nil)
			_, err = io.Copy(buf, file)
			require.NoError(b, err)

			doc := fulltext.IndexDoc{
				Id:         []byte(path),
				IndexValue: buf.Bytes(),
			}

			b.StartTimer()
			idx.AddDoc(doc)
			b.StopTimer()

			return nil
		})

		outbuf := bytes.NewBuffer(nil)
		err := idx.FinalizeAndWrite(outbuf)
		require.NoError(b, err)
	}
}

type bleveDoc struct {
	Path     string
	Contents string
}

// benchmarkWithBleve ...
func benchmarkWithBleve(b *testing.B, path string) {
	tmpDir, err := os.MkdirTemp("./testdata", "bleve")
	require.NoError(b, err)

	b.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	mapping := bleve.NewIndexMapping()
	idx, err := bleve.New(tmpDir, mapping)
	require.NoError(b, err)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dirfs := os.DirFS(path)

		fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, err error) error {
			if path == "." || d.IsDir() {
				return nil
			}

			b.StopTimer()
			file, err := dirfs.Open(path)
			if err != nil {
				return fmt.Errorf("unable to open file %q; %w", path, err)
			}

			buf := bytes.NewBuffer(nil)
			_, err = io.Copy(buf, file)
			require.NoError(b, err)

			doc := bleveDoc{path, buf.String()}

			defer b.StopTimer()
			b.StartTimer()
			return idx.Index(doc.Path, doc)
		})
	}
}
