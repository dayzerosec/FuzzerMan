package gsutil

import (
	"cloud.google.com/go/storage"
	"errors"
	"google.golang.org/api/iterator"
	"io/ioutil"
	"os/exec"
	"strings"
	"time"
)

// parseGsutilError attempts to turn a CLI error message into a meaningful error to return
func (c *Client) parseGsutilError(output []byte) error {
	for _, line := range strings.Split(string(output), "\n") {
		exceptionIndex := strings.Index(line, "Exception: ")
		if exceptionIndex >= 0 {
			return errors.New(line)
		}
	}
	return nil
}

func (c *Client) runGsutilCommand(args []string) ([]byte, error) {
	cmd := exec.CommandContext(c.context, "gsutil", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if gsutilErr := c.parseGsutilError(out); gsutilErr != nil {
			return []byte{}, gsutilErr
		}
		return []byte{}, err
	}
	return out, nil
}

// Mirror will clone src into dst. This includes deleting any files that are present in dst but not src. If you want
// to just move new files to dst use `Copy`
func (c *Client) Mirror(src, dst string, recursive bool) error {
	args := []string{"-m", "rsync"}
	//args = append(args, "-n") //dry-run
	args = append(args, "-j", "txt,log") //enables compression
	args = append(args, "-d")            //allows deletion
	args = append(args, "-c")            //use checksums instead of mtime since merges screw with mtime
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, src, dst)
	_, err := c.runGsutilCommand(args)
	return err
}

// Copy can be used to copy a file (including globing) from one location to another. Unlike mirror it will not delete
// any content from the destination
func (c *Client) Copy(src, dst string, recursive bool) error {
	return c.CopyMulti([]string{src}, dst, recursive)
}

// CopyMulti can be used to copy multiple files by name to a single destination folder
func (c *Client) CopyMulti(files []string, dst string, recursive bool) error {
	args := []string{"-m", "cp"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, files...)
	args = append(args, dst)
	_, err := c.runGsutilCommand(args)
	return err
}

// Object uses the storage library to an ObjectHandle to the given location
func (c *Client) Object(location string) (*storage.ObjectHandle, error) {
	if !strings.HasPrefix(location, "gs://") {
		return nil, errors.New("Object location must start with gs://")
	}
	pathParts := strings.SplitN(location[5:], "/", 2)
	bucketId := pathParts[0]
	file := pathParts[1]

	bucket := c.client.Bucket(bucketId)
	object := bucket.Object(file)
	return object, nil

}

// FileInfo uses the storage library to retrieve the object's attribute
func (c *Client) FileInfo(location string) (*storage.ObjectAttrs, error) {
	object, err := c.Object(location)
	if err != nil {
		return nil, err
	}
	return object.Attrs(c.context)
}

func (c *Client) ReadFile(location string) ([]byte, error) {
	if !strings.HasPrefix(location, "gs://") {
		return []byte{}, errors.New("ReadFile location must start with gs://")
	}
	pathParts := strings.SplitN(location[5:], "/", 2)
	bucketId := pathParts[0]
	file := pathParts[1]

	b := c.client.Bucket(bucketId)
	objHandle := b.Object(file)
	reader, err := objHandle.NewReader(c.context)
	if err != nil {
		return []byte{}, err
	}
	defer func() { _ = reader.Close() }()

	return ioutil.ReadAll(reader)
}

func (c *Client) WriteFile(location string, buf []byte) error {
	if !strings.HasPrefix(location, "gs://") {
		return errors.New("WriteFile location must start with gs://")
	}
	pathParts := strings.SplitN(location[5:], "/", 2)
	bucketId := pathParts[0]
	file := pathParts[1]

	b := c.client.Bucket(bucketId)
	objHandle := b.Object(file)
	writer := objHandle.NewWriter(c.context)
	_, err := writer.Write(buf)
	if err != nil {
		_ = writer.Close()
		return err
	}

	// Upload is finalized on close so, if there is an error here it failed
	err = writer.Close()
	return err

}

// NewObjects returns a list of new objects in the location since a given timestamp
// location should be a full URL (gs://<bucket>/<path>) to a directory to search
func (c *Client) NewObjects(location string, since time.Time) ([]*storage.ObjectAttrs, error) {
	if !strings.HasPrefix(location, "gs://") {
		return nil, errors.New("WriteFile location must start with gs://")
	}
	pathParts := strings.SplitN(location[5:], "/", 2)
	bucketId := pathParts[0]
	file := pathParts[1]

	b := c.client.Bucket(bucketId)
	objs := b.Objects(c.context, &storage.Query{
		Prefix: file,
	})
	var newFiles []*storage.ObjectAttrs
	for {
		attrs, err := objs.Next()
		if err != nil {
			if err == iterator.Done {
				break
			} else {
				return nil, err
			}
		}

		if attrs.Created.After(since) {
			newFiles = append(newFiles, attrs)
		}
	}
	return newFiles, nil
}
