package cloudutil

import (
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (c *Client) UploadIfNotExist(localFolder string, files []string, prefix string) error {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return err
	}

	var newFiles []string
	for _, fn := range files {
		key := path.Join(prefix, fn)
		if exists, _ := b.Exists(c.context, key); !exists {
			newFiles = append(newFiles, fn)
		}
	}
	return c.Upload(localFolder, newFiles, prefix)
}

func (c *Client) uploadFile(b *blob.Bucket, key, localFn string) {
	defer c.wg.Done()
	if err := c.sema.Acquire(c.context, 1); err != nil {
		log.Printf("[!] failed to acquire semaphore(download: %s): %s", key, err.Error())
		return
	}
	defer c.sema.Release(1)

	if _, err := os.Stat(localFn); err != nil {
		log.Printf("[!] Failed to upload(%s): %s", localFn, err.Error())
		return
	}

	data, err := os.ReadFile(localFn)
	if err != nil {
		log.Printf("[!] Failed to read upload target(%s): %s", localFn, err.Error())
		return
	}

	writer, err := b.NewWriter(c.context, key, nil)
	if err != nil {
		log.Printf("[!] failed to upload(%s): %s", key, err.Error())
		return
	}
	_, _ = writer.Write(data)
	err = writer.Close()
	if err != nil {
		log.Printf("[!] upload write failed(%s): %s", key, err.Error())
		return
	}
}

func (c *Client) Upload(localFolder string, files []string, prefix string) error {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return err
	}

	localFolder, _ = filepath.Abs(localFolder)
	c.wg.Add(len(files))
	for _, fn := range files {
		localFn := filepath.Join(localFolder, fn)
		key := path.Join(prefix, fn)
		go c.uploadFile(b, key, localFn)
	}
	c.wg.Wait()
	log.Println("[-] Upload finished")
	return nil
}

func (c *Client) DownloadSingle(key string, localFile string) error {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return err
	}

	reader, err := b.NewReader(c.context, key, nil)
	if err != nil {
		return err
	}

	if fp, err := os.OpenFile(localFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0770); err != nil {
		log.Printf("[!] failed to open(%s): %s", key, err.Error())
	} else {
		if _, err = reader.WriteTo(fp); err != nil {
			log.Printf("[!] failed to write: %s", err.Error())
		}
		_ = fp.Close()
	}
	_ = reader.Close()
	return nil

}

func (c *Client) downloadFile(b *blob.Bucket, key, localFn string) {
	defer c.wg.Done()
	if err := c.sema.Acquire(c.context, 1); err != nil {
		log.Printf("[!] failed to acquire semaphore(download: %s): %s", key, err.Error())
		return
	}
	defer c.sema.Release(1)

	reader, err := b.NewReader(c.context, key, nil)
	if err != nil {
		log.Printf("[!] failed to open reader(%s): %s", key, err.Error())
		return
	}
	defer func() { _ = reader.Close() }()

	if fp, err := os.OpenFile(localFn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660); err != nil {
		log.Printf("[!] failed to open(%s): %s", key, err.Error())
	} else {
		if _, err = reader.WriteTo(fp); err != nil {
			log.Printf("[!] failed to write: %s", err.Error())
		}
		_ = fp.Close()
	}
}

func (c *Client) Download(keys []string, localFolder string) error {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return err
	}

	localFolder, _ = filepath.Abs(localFolder)
	c.wg.Add(len(keys))
	for _, key := range keys {
		localFn, _ := filepath.Abs(filepath.Join(localFolder, filepath.Base(key)))
		go c.downloadFile(b, key, localFn)
	}
	c.wg.Wait()
	return nil
}

// FileInfo uses the storage library to retrieve the object's attribute
func (c *Client) FileInfo(key string) (*blob.Attributes, error) {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return nil, err
	}

	return b.Attributes(c.context, key)
}

func (c *Client) ReadFile(key string, opts *blob.ReaderOptions) ([]byte, error) {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return nil, err
	}

	reader, err := b.NewReader(c.context, key, opts)
	if err != nil {
		return []byte{}, err
	}
	defer func() { _ = reader.Close() }()

	return io.ReadAll(reader)
}

func (c *Client) WriteFile(key string, buf []byte, opts *blob.WriterOptions) error {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return err
	}

	writer, err := b.NewWriter(c.context, key, opts)
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	if _, err = writer.Write(buf); err != nil {
		return err
	}
	err = writer.Close()
	return err
}

// NewObjects returns a list of new objects in the location since a given timestamp
func (c *Client) NewObjects(prefix string, since time.Time) ([]*blob.ListObject, error) {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return nil, err
	}

	iter := b.List(nil)
	var out []*blob.ListObject
	for {
		obj, err := iter.Next(c.context)
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}

		if prefix == "" || strings.HasPrefix(obj.Key, prefix) {
			if !obj.IsDir && obj.ModTime.After(since) {
				out = append(out, obj)
			}
		}
	}
	return out, nil
}

// MirrorLocal mirrors the remotePrefix in localFolder, this can delete files from localFolder
// Any folders under the prefix will be flattened, and if a file already exists it is simply
// not downloaded, there is no checksum or mtime comparison
func (c *Client) MirrorLocal(remotePrefix, localFolder string) (int, int, error) {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return -1, -1, err
	}

	// Find all the files in remote not in local
	remoteFiles := make(map[string]bool)
	var toDownload []string

	iter := b.List(&blob.ListOptions{Prefix: remotePrefix})
	for {
		obj, err := iter.Next(c.context)
		if err == io.EOF {
			break
		}
		if err != nil || obj.IsDir {
			continue
		}

		remoteFiles[filepath.Base(obj.Key)] = true
		localFn := filepath.Join(localFolder, filepath.Base(obj.Key))
		if _, err := os.Stat(localFn); os.IsNotExist(err) {
			// We don't have this file so download
			toDownload = append(toDownload, obj.Key)
		}
	}

	// Find all files present in local but not in remote and delete identified files
	var toDelete []string
	files, err := os.ReadDir(localFolder)
	if err != nil {
		return -1, -1, err
	}
	for _, f := range files {
		if !f.IsDir() {
			if _, found := remoteFiles[f.Name()]; !found {
				toDelete = append(toDelete, f.Name())
			}
		}
	}

	// Perform the actions...
	if len(toDownload) > 0 {
		_ = c.Download(toDownload, localFolder)
	}

	if len(toDelete) > 0 {
		for _, fn := range toDelete {
			_ = os.Remove(filepath.Join(localFolder, fn))
		}
	}

	return len(toDownload), len(toDelete), nil
}

// MirrorRemote will make the remote prefix match the local folder including deleting files from remote
func (c *Client) MirrorRemote(localFolder, remotePrefix string) (int, int, error) {
	b, err := blob.OpenBucket(c.context, c.bucket)
	if err != nil {
		return -1, -1, err
	}

	// Find all the files in local but not in remote
	var toUpload []string
	localFiles := make(map[string]bool)
	files, err := os.ReadDir(localFolder)
	if err != nil {
		return -1, -1, err
	}
	for _, f := range files {
		if !f.IsDir() {
			localFiles[f.Name()] = true
			if exists, _ := b.Exists(c.context, path.Join(remotePrefix, f.Name())); !exists {
				toUpload = append(toUpload, f.Name())
			}
		}
	}

	// Find all files present in remote but not in local
	var toDelete []string
	iter := b.List(&blob.ListOptions{Prefix: remotePrefix})
	for {
		obj, err := iter.Next(c.context)
		if err == io.EOF {
			break
		}
		if err != nil || obj.IsDir {
			continue
		}
		if _, found := localFiles[filepath.Base(obj.Key)]; !found {
			toDelete = append(toDelete, obj.Key)
		}
	}

	// Perform the actions
	_ = c.Upload(localFolder, toUpload, remotePrefix)
	for _, key := range toDelete {
		_ = b.Delete(c.context, key)
	}

	return len(toUpload), len(toDelete), nil
}
