package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
)

// initGcs only creates a buckethandle for the exact bucket needed in this project.
func initGcs() error {
	log.Println("initGcs", c.ENV, c.ServiceAccountJson, c.BucketName)
	var err error
	ctx = context.Background()
	gcs, err = storage.NewClient(ctx, option.WithCredentialsFile(c.ServiceAccountJson))
	if err != nil {
		return err
	}
	// Sets the name for the new bucket.
	// Creates a Bucket instance.
	bucket = gcs.Bucket(c.BucketName)
	return err
}

// gcsUp pushes dbPath up to c.GcsTopPath + dbPath
func gcsUp(dbPath string) error {
	f, err := os.Open(dbPath)
	if err != nil {
		log.Println("Failed opening file ", dbPath)
		return err
	}
	fullpath := filepath.Join(c.GcsTopPath, dbPath)

	log.Println("Uploading ", dbPath, " to ", fullpath)

	wc := bucket.Object(fullpath).NewWriter(ctx)
	wc.ContentType = "binary"
	r4 := bufio.NewReader(f)

	count, err := io.Copy(wc, r4)
	if err != nil {
		log.Println("gcsUp ", fullpath, err)
		return err
	}

	log.Println("Copied ", count, " bytes.")

	return wc.Close()
}

// gcsDown pulls c.GcsTopPath + `/` + dbPath down to dbPath
func gcsDown(dbPath string) {

	dbDir := filepath.Dir(dbPath)
	_ = os.MkdirAll(dbDir, os.ModePerm)
	f, err := os.Create(dbPath) // path on current environment
	if err != nil {
		log.Fatal(dbPath, err)
	}

	//	fullpath := c.GcsTopPath + `/` + dbPath // path on gcs
	fullpath := filepath.Join(c.GcsTopPath, dbPath) // path on gcs
	ob, err := bucket.Object(fullpath).NewReader(ctx)
	if err != nil {
		log.Fatal(fullpath, " ", err)
	}

	fi := bufio.NewWriter(f)

	bytecount, err := io.Copy(fi, ob)
	if err != nil {
		log.Println("copy file from gcs failed", err)
	}

	log.Println(bytecount, " bytes copied. ", fullpath)
	if bytecount == 0 {
		log.Println("gcsDown failed.", bytecount, " copied")
		log.Fatal("gcsDown failed ", fullpath)
	}
}
