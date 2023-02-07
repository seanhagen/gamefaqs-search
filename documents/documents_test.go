package documents

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/blevesearch/bleve"
	"github.com/blugelabs/bluge"
	"github.com/seanhagen/fulltext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchingWithFulltextReturnsExpectedDocuments(t *testing.T) {
	idx, err := fulltext.NewIndexer("")
	require.NoError(t, err)
	idx.StopWordCheck = fulltext.EnglishStopWordChecker

	dirfs := os.DirFS("./testdata/randomly_picked")
	err = fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, errx error) error {
		if d.IsDir() {
			return nil
		}
		t.Logf("adding %q to index", path)
		file, err := dirfs.Open(path)
		if err != nil {
			return fmt.Errorf("unable to open file %q; %w", path, err)
		}

		bits, err := io.ReadAll(file)
		if err != nil {
			return fmt.Errorf("unable to read file: %w", err)
		}

		doc := fulltext.IndexDoc{
			Id:         []byte(path),
			IndexValue: bits,
		}
		return idx.AddDoc(doc)
	})
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("./testdata", "fulltext-search")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp(tmpDir, "index")
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, tmpFile.Close())
		os.RemoveAll(tmpDir)
	})

	err = idx.FinalizeAndWrite(tmpFile)
	require.NoError(t, err)

	idx.Close()

	tests := []struct {
		query  string
		expect string
		num    int
	}{
		{
			query:  "Shadow Man is one of the hardest to fight",
			expect: "564394-mega-man-the-wily-wars-faqs-59628.txt",
			num:    5,
		},
		{
			query:  "Shadow Blades Jump",
			expect: "564394-mega-man-the-wily-wars-faqs-59628.txt",
			num:    2,
		},
	}

	_, err = tmpFile.Seek(0, 0)
	require.NoError(t, err)

	search, err := fulltext.BetterNewSearcher(tmpFile)
	require.NoError(t, err)

	for i, x := range tests {
		tt := x
		t.Run(fmt.Sprintf("sb%v query %q", i, tt.query), func(t *testing.T) {
			results, err := search.SimpleSearch(tt.query, tt.num)
			require.NoError(t, err)
			assert.NotEmpty(t, results.Items)

			names := []string{}
			for _, v := range results.Items {
				names = append(names, string(v.Id))
			}
			assert.Contains(t, names, tt.expect, "couldn't find expected document name in results")
		})
	}
}

func BenchmarkSearchingWithBluge(b *testing.B) {
	tmpDir, err := os.MkdirTemp("./testdata", "bleve")
	require.NoError(b, err)

	b.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	config := bluge.DefaultConfig(tmpDir)
	writer, err := bluge.OpenWriter(config)
	require.NoError(b, err)

	dirfs := os.DirFS("./testdata/randomly_picked")

	fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
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

		doc := bluge.NewDocument(path).
			AddField(bluge.NewTextField("name", path)).
			AddField(bluge.NewNumericField("size", float64(info.Size()))).
			AddField(bluge.NewTextField("contents", buf.String()))

		return writer.Update(doc.ID(), doc)
	})

	search, err := writer.Reader()
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := "Shadow Man is one of the hardest to fight"
		matchQuery := bluge.NewMatchQuery(query).SetField("contents")
		request := bluge.NewTopNSearch(10, matchQuery).WithStandardAggregations()

		iter, err := search.Search(context.TODO(), request)
		require.NoError(b, err)
		require.NotNil(b, iter)
	}
}

func BenchmarkSearchingWithBleve(b *testing.B) {
	tmpDir, err := os.MkdirTemp("./testdata", "bleve")
	require.NoError(b, err)

	b.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	mapping := bleve.NewIndexMapping()
	idx, err := bleve.New(tmpDir, mapping)
	require.NoError(b, err)

	dirfs := os.DirFS("./testdata/randomly_picked")

	err = fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, errx error) error {
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

		doc := bleveDoc{path, buf.String()}
		return idx.Index(doc.Path, doc)
	})
	require.NoError(b, err)
	require.NoError(b, idx.Close())

	search, err := bleve.Open(tmpDir)
	require.NoError(b, err)

	query := "Shadow Man is one of the hardest to fight"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		searchQuery := bleve.NewQueryStringQuery(query)
		searchRequest := bleve.NewSearchRequest(searchQuery)
		results, err := search.Search(searchRequest)
		require.NoError(b, err)
		assert.NotEmpty(b, results)
	}

}

// BenchmarkSearchingWithFulltext ...
func BenchmarkSearchingWithFulltext(b *testing.B) {
	idx, err := fulltext.NewIndexer("")
	require.NoError(b, err)
	defer idx.Close()

	idx.StopWordCheck = fulltext.EnglishStopWordChecker

	dirfs := os.DirFS("./testdata/randomly_picked")
	err = fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, errx error) error {
		if d.IsDir() {
			return nil
		}

		file, err := dirfs.Open(path)
		if err != nil {
			return fmt.Errorf("unable to open file %q; %w", path, err)
		}

		bits, err := io.ReadAll(file)
		if err != nil {
			return fmt.Errorf("unable to read file: %w", err)
		}

		doc := fulltext.IndexDoc{
			Id:         []byte(path),
			IndexValue: bits,
		}
		return idx.AddDoc(doc)
	})
	require.NoError(b, err)

	tmpDir, err := os.MkdirTemp("./testdata", "fulltext-search")
	require.NoError(b, err)

	tmpFile, err := os.CreateTemp(tmpDir, "index")
	require.NoError(b, err)

	b.Cleanup(func() {
		require.NoError(b, tmpFile.Close())
		os.RemoveAll(tmpDir)
	})

	err = idx.FinalizeAndWrite(tmpFile)
	require.NoError(b, err)

	idx.Close()

	query := "Shadow Man is one of the hardest to fight"

	_, err = tmpFile.Seek(0, 0)
	require.NoError(b, err)

	search, err := fulltext.BetterNewSearcher(tmpFile)
	require.NoError(b, err)

	for i := 0; i < b.N; i++ {
		_, err := search.SimpleSearch(query, 5)
		require.NoError(b, err)
	}
}

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

	b.Cleanup(func() {
		os.RemoveAll(confPath)
	})

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

		err := fs.WalkDir(dirfs, ".", func(path string, d fs.DirEntry, err error) error {
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
		require.NoError(b, err)

		outbuf := bytes.NewBuffer(nil)
		err = idx.FinalizeAndWrite(outbuf)
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
