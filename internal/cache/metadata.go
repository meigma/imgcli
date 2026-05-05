package cache

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type blobMetadata struct {
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"lastUsed"`
}

type pruneCandidate struct {
	path     string
	sha256   string
	size     int64
	lastUsed time.Time
}

func (s *DiskStore) touchBlob(blob Blob) error {
	metadata := blobMetadata{
		SHA256:   blob.SHA256,
		Size:     blob.Size,
		LastUsed: time.Now().UTC(),
	}
	if err := s.writeMetadata(metadata); err != nil {
		return fmt.Errorf("write cache metadata: %w", err)
	}

	return nil
}

func (s *DiskStore) writeMetadata(metadata blobMetadata) error {
	path := s.metadataPath(metadata.SHA256)
	if err := ensureCacheDir(filepath.Dir(path), dirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+metadata.SHA256+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create cache metadata temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode cache metadata: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cache metadata temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, metadataPerm); err != nil {
		return fmt.Errorf("set cache metadata permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish cache metadata: %w", err)
	}
	cleanup = false

	return nil
}

// Prune removes least-recently-used cached blobs until the store is at or below
// the configured maximum size.
//
// Prune is explicit maintenance. Fetch does not prune because it returns cache
// paths directly, and automatic eviction could invalidate paths held by another
// caller or process.
func (s *DiskStore) Prune(ctx context.Context) error {
	if s.maxSizeBytes == 0 {
		return nil
	}

	candidates, totalSize, err := s.pruneCandidates()
	if err != nil || totalSize <= s.maxSizeBytes {
		return err
	}

	slices.SortFunc(candidates, func(a pruneCandidate, b pruneCandidate) int {
		if cmp := a.lastUsed.Compare(b.lastUsed); cmp != 0 {
			return cmp
		}
		if a.sha256 < b.sha256 {
			return -1
		}
		if a.sha256 > b.sha256 {
			return 1
		}
		return 0
	})

	remaining := len(candidates)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if totalSize <= s.maxSizeBytes {
			return nil
		}
		if remaining <= 1 {
			return nil
		}

		if err := os.Remove(candidate.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		_ = os.Remove(s.metadataPath(candidate.sha256))
		totalSize -= candidate.size
		remaining--
	}

	return nil
}

func (s *DiskStore) pruneCandidates() ([]pruneCandidate, int64, error) {
	root := filepath.Join(s.root, "blobs", "sha256")
	var candidates []pruneCandidate
	var totalSize int64

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		digest := entry.Name()
		if !isSHA256Hex(digest) {
			return nil
		}

		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		size := info.Size()
		totalSize += size
		metadata, ok := s.readMetadata(digest, size)
		lastUsed := info.ModTime().UTC()
		if ok {
			lastUsed = metadata.LastUsed
		}

		candidates = append(candidates, pruneCandidate{
			path:     path,
			sha256:   digest,
			size:     size,
			lastUsed: lastUsed,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	return candidates, totalSize, nil
}

func (s *DiskStore) readMetadata(sha256Digest string, size int64) (blobMetadata, bool) {
	data, err := os.ReadFile(s.metadataPath(sha256Digest))
	if err != nil {
		return blobMetadata{}, false
	}

	var metadata blobMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return blobMetadata{}, false
	}
	if metadata.SHA256 != sha256Digest || metadata.Size != size || metadata.LastUsed.IsZero() {
		return blobMetadata{}, false
	}

	return metadata, true
}

func (s *DiskStore) metadataPath(sha256Digest string) string {
	return filepath.Join(s.root, "metadata", "sha256", sha256Digest[:digestShardLength], sha256Digest+".json")
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256HexLength {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
