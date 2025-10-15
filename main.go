package main

import (
	"crypto/md5"
	"os"
)

const (
	CREATE  = false // test
	FAKEMD5 = false // test
)

// this is a simple demonstration of creating and reading a PAC file.
// the code for reading and writing needs to be cleaned up a lot. this is just a quick demo.
func main() {
	if CREATE {
		pac := NewPAC(1000, []byte("hi"), 0)
		err := pac.GetReady()
		if err != nil {
			panic(err)
		}

		data := []byte("Hello, World!")
		theMD5 := md5.Sum(data)

		if FAKEMD5 {
			// modify the md5 to be all zeros for testing
			theMD5 = [16]byte{}
		}

		pac.AddEntry(
			Entry{
				Path:  "/hello.txt",
				Flags: 0,
				Type:  0,
				MD5:   theMD5,
			},
			data,
		)

		res, err := pac.Save()
		defer pac.Close()
		if err != nil {
			panic(err)
		}

		err = os.WriteFile("archive.pac", res, 0644)
		if err != nil {
			panic(err)
		}

		println("PAC file created: archive.pac")
	} else {
		pac, err := LoadPACFromFile("archive.pac")
		if err != nil {
			panic(err)
		}
		for _, file := range pac.FileTable.Entries {
			println("Path:", file.Path)
			println("Size:", file.Length)
		}

		d, success := pac.GetEntryDataByPath("/hello.txt")
		if !success {
			panic("file not found")
		}
		println("Data:", string(d))
	}
}
