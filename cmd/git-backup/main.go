package main

import (
	"crypto/tls"
	"flag"
	gitbackup "git-backup"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"context"
	"fmt"
)

var configFilePath = flag.String("config.file", "git-backup.yml", "The path to your config file.")
var targetPath = flag.String("backup.path", "backup", "The target path to the backup folder.")
var failAtEnd = flag.Bool("backup.fail-at-end", false, "Fail at the end of backing up repositories, rather than right away.")
var bareClone = flag.Bool("backup.bare-clone", false, "Make bare clones without checking out the main branch.")
var printVersion = flag.Bool("version", false, "Show the version number and exit.")
var enableInsecure = flag.Bool("insecure", false, "Use this flag to disable verification of SSL/TLS certificates")

var Version = "dev"
var CommitHash = "n/a"
var BuildTimestamp = "n/a"

func main() {
	flag.Parse()
	log.Printf("inscure: %v", *enableInsecure)

	if *printVersion {
		log.Printf("git-backup, version %s (%s-%s)", Version, runtime.GOOS, runtime.GOARCH)
		log.Printf("Built %s (%s)", CommitHash, BuildTimestamp)
		os.Exit(0)
	}

	if *enableInsecure {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	config := loadConfig()
	sources := config.GetSources()
	if len(sources) == 0 {
		log.Printf("Found a config file at [%s] but detected no sources. Are you sure the file is properly formed?", *configFilePath)
		os.Exit(111)
	}
	repoCount := 0
	errors := 0
	backupStart := time.Now()
	for _, source := range sources {
		sourceName := source.GetName()
		log.Printf("=== %s ===", sourceName)
		if err := source.Test(); err != nil {
			log.Printf("Failed to verify connection to job [%s]: %s", sourceName, err)
			os.Exit(110)
		}
		repos, err := source.ListRepositories()
		if err != nil {
			log.Printf("Communication Error: %s", err)
			os.Exit(100)
		}
		for _, repo := range repos {
			log.Printf("Discovered %s", repo.FullName)
			targetPath := filepath.Join(*targetPath, sourceName, repo.FullName)
			err := os.MkdirAll(targetPath, os.ModePerm)
			if err != nil {
				log.Printf("Failed to create directory: %s", err)
				os.Exit(100)
			}
			err = repo.CloneInto(targetPath, *bareClone)
			if err != nil {
				errors++
				log.Printf("Failed to clone: %s", err)
				if *failAtEnd == false {
					os.Exit(100)
				}
			}
			repoCount++
		}
	}
	log.Printf("Backed up %d repositories in %s, encountered %d errors", repoCount, time.Now().Sub(backupStart), errors)

	if errors > 0 {
		os.Exit(100)
	}
}

func loadConfig() gitbackup.Config {
	// try config file in working directory
	config, err := gitbackup.LoadFile(*configFilePath)
	if os.IsNotExist(err) {
		log.Println("No config file found. Exiting...")
		os.Exit(1)
	} else if err != nil {
		log.Printf("Error: %s", err)
		os.Exit(1)
	}
	return config
}

func uploadBackup() {
	fmt.Printf("Preparing to upload to Azure Storage\n")

	// TODO: replace <storage-account-name> with your actual storage account name
	url := "https://ambackupstorage.blob.core.windows.net/"
	ctx := context.Background()

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	handleError(err)

	client, err := azblob.NewClient(url, credential, nil)
	handleError(err)

	err = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }

        if !info.IsDir() {
            relativePath, err := filepath.Rel(targetPath, path)
            if err != nil {
                return err
            }

            blobClient := containerClient.NewBlockBlobClient(relativePath)
            file, err := os.Open(path)
            if err != nil {
                return err
            }
            defer file.Close()

            _, err = blobClient.Upload(context.Background(), file, nil)
            if err != nil {
                return err
            }

            fmt.Printf("Uploaded %s\n", relativePath)
        }
        return nil
    })

    if err != nil {
        log.Fatalf("Failed to upload folder: %v", err)
    }

    fmt.Println("Upload complete")
}
