package main

import (
	"bytes"
	"container/list"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"
)

var pacSaveBuf bytes.Buffer
var dataPosOfs uint32
var tablePosOfs uint64

var dataPos uint32

var entries *list.List

// NOTE: This is not the exact structure of a PAC file on disk.
// Please refer to SPEC.md for more information.
type PAC struct {
	Format      uint32 // Format version
	Version     uint32 // Version number, set by saver.
	PackagedFor []byte // Intended target. (e.g. "MyModLoader" or whatever you want)
	Flags       uint32 // Bit flags.

	FileDataPos  uint32 // File data position.
	FileTablePos uint64 // File table position.

	FileData  []byte    // File data.
	FileTable FileTable // File table.
}

type FileTable struct {
	Entries []Entry // list of entries.
}

type Entry struct {
	Path string // Path (e.g. /dir/file.txt). Must start with a forward slash.

	Flags uint32 // Entry flags
	Type  uint32 // Type of asset

	MD5 [16]byte // MD5 hash

	Offset uint64 // Offset in file data
	Length uint64 // Length of file data
}

func (pac *PAC) GetReady() error {
	var err error

	pacSaveBuf.Reset()
	entries = list.New()

	dataPosOfs = 0
	tablePosOfs = 0

	dataPos = 0

	// Write magic number
	_, err = pacSaveBuf.Write([]byte(MAGIC))
	// Write format version
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, pac.Format)
	// Write version
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, pac.Version)
	// Write packaged for (length-prefixed byte array)
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, uint16(len(pac.PackagedFor)))
	pacSaveBuf.Write(pac.PackagedFor)
	// Write flags
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, pac.Flags)
	// Write reserved fields (2 * uint32)
	var reserved [2]uint32
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, reserved)

	// Placeholder for file data position
	// dataPosOfs = where we are now
	dataPosOfs = uint32(pacSaveBuf.Len())
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, uint32(0))
	// Placeholder for file table position
	// tablePosOfs = where we are now
	tablePosOfs = uint64(pacSaveBuf.Len())
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, uint64(0))

	// Write reserved fields (2 * uint32)
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, reserved)

	// Now, the file data will be written after this. Get current position.
	dataPos = uint32(pacSaveBuf.Len())
	// overwrite the file data pos placeholder with the actual position
	binary.LittleEndian.PutUint32(pacSaveBuf.Bytes()[dataPosOfs:], dataPos)

	if err != nil {
		return err
	}
	return nil
}

func (pac *PAC) AddEntry(entry Entry, data []byte) {
	var le Entry

	last := entries.Back()
	// last might be nil if this is the first entry
	if last == nil {
		le = Entry{
			Offset: 0,
			Length: 0,
		}
	} else {
		le = last.Value.(Entry)
	}

	entry.Length = uint64(len(data))
	entry.Offset = le.Offset + le.Length

	// add entry to list
	entries.PushBack(entry)
	// write data to buffer
	pacSaveBuf.Write(data)
}

func (pac *PAC) Save() ([]byte, error) {
	var err error

	// get current position (this is where the file table starts)
	tablePos := uint64(pacSaveBuf.Len())
	// overwrite the file table pos placeholder with the actual position
	binary.LittleEndian.PutUint64(pacSaveBuf.Bytes()[tablePosOfs:], tablePos)

	// write entry count
	entryCount := uint64(entries.Len())
	err = binary.Write(&pacSaveBuf, binary.LittleEndian, entryCount)

	// write entries
	for e := entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(Entry)
		// write path (length-prefixed byte array)
		err = binary.Write(&pacSaveBuf, binary.LittleEndian, uint16(len(entry.Path)))
		pacSaveBuf.Write([]byte(entry.Path))
		// write flags
		err = binary.Write(&pacSaveBuf, binary.LittleEndian, entry.Flags)
		// write type
		err = binary.Write(&pacSaveBuf, binary.LittleEndian, entry.Type)
		// write MD5
		pacSaveBuf.Write(entry.MD5[:])
		// write offset
		err = binary.Write(&pacSaveBuf, binary.LittleEndian, entry.Offset)
		// write length
		err = binary.Write(&pacSaveBuf, binary.LittleEndian, entry.Length)
	}

	if err != nil {
		return nil, err
	}
	return pacSaveBuf.Bytes(), nil
}

func (pac *PAC) Close() {
	entries = nil
	pacSaveBuf.Reset()

	dataPosOfs = 0
	tablePosOfs = 0

	dataPos = 0
}

func (pac *PAC) GetEntryData(entry Entry) []byte {
	return pac.FileData[entry.Offset : entry.Offset+entry.Length]
}

func (pac *PAC) GetEntryDataByPath(path string) ([]byte, bool) {
	for _, entry := range pac.FileTable.Entries {
		if entry.Path == path {
			return pac.GetEntryData(entry), true
		}
	}
	return nil, false
}

func NewPAC(saverVersion uint32, four []byte, flags uint32) *PAC {
	return &PAC{
		Format:  FMT_VERSION,
		Version: saverVersion,

		PackagedFor: four,

		Flags: flags,
	}
}

func LoadPAC(data []byte) (*PAC, error) {
	// read first 4 bytes to check magic number ("PACC")
	// things are little-endian, so read accordingly.
	reader := bytes.NewReader(data)

	var pac PAC
	var magic [4]byte
	_, err := reader.Read(magic[:])
	if magic != [4]byte{'P', 'A', 'C', 'C'} {
		return nil, fmt.Errorf("invalid magic number: got %s, expected %s", string(magic[:]), MAGIC)
	}

	println("Magic number OK.")

	// format and version
	if err := binary.Read(reader, binary.LittleEndian, &pac.Format); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &pac.Version); err != nil {
		return nil, err
	}

	println("Format version:", pac.Format)
	println("Saver version:", pac.Version)

	// packaged for (length-prefixed byte array)
	pac.PackagedFor, err = readArray(reader)
	if err != nil {
		return nil, err
	}

	println("Packaged for:", *(*string)(unsafe.Pointer(&pac.PackagedFor)))

	// flags
	if err := binary.Read(reader, binary.LittleEndian, &pac.Flags); err != nil {
		return nil, err
	}

	println("Flags:", pac.Flags)

	// reserved fields (2 * uint32)
	var reserved [2]uint32
	if err := binary.Read(reader, binary.LittleEndian, &reserved); err != nil {
		return nil, err
	}

	// file data position
	if err := binary.Read(reader, binary.LittleEndian, &pac.FileDataPos); err != nil {
		return nil, err
	}
	// file table position
	if err := binary.Read(reader, binary.LittleEndian, &pac.FileTablePos); err != nil {
		return nil, err
	}

	println("File data position:", pac.FileDataPos)
	println("File table position:", pac.FileTablePos)

	// reserved fields (2 * uint32)
	if err := binary.Read(reader, binary.LittleEndian, &reserved); err != nil {
		return nil, err
	}

	// seek to file data
	if _, err := reader.Seek(int64(pac.FileDataPos), io.SeekStart); err != nil {
		return nil, err
	}
	// read file data
	pac.FileData = make([]byte, pac.FileTablePos-uint64(pac.FileDataPos))
	if _, err := io.ReadFull(reader, pac.FileData); err != nil {
		return nil, err
	}

	// go to file table position
	if _, err := reader.Seek(int64(pac.FileTablePos), io.SeekStart); err != nil {
		return nil, err
	}
	// read entry count
	var entryCount uint64
	if err := binary.Read(reader, binary.LittleEndian, &entryCount); err != nil {
		return nil, err
	}
	println("Entry count:", entryCount)

	// read entries
	pac.FileTable.Entries = make([]Entry, entryCount)
	for i := uint64(0); i < entryCount; i++ {
		entry := &pac.FileTable.Entries[i]
		// read path (length-prefixed byte array)
		pathBytes, err := readArray(reader)
		if err != nil {
			return nil, err
		}
		entry.Path = (string)(pathBytes)

		// read flags
		if err := binary.Read(reader, binary.LittleEndian, &entry.Flags); err != nil {
			return nil, err
		}
		// read type
		if err := binary.Read(reader, binary.LittleEndian, &entry.Type); err != nil {
			return nil, err
		}
		// read MD5
		if _, err := reader.Read(entry.MD5[:]); err != nil {
			return nil, err
		}
		// read offset
		if err := binary.Read(reader, binary.LittleEndian, &entry.Offset); err != nil {
			return nil, err
		}
		// read length
		if err := binary.Read(reader, binary.LittleEndian, &entry.Length); err != nil {
			return nil, err
		}

		// validate md5
		actualMD5 := md5.Sum(pac.GetEntryData(*entry))
		if actualMD5 != entry.MD5 {
			return nil, fmt.Errorf("md5 mismatch for entry %s (got %x, expected %x)", entry.Path, entry.MD5, actualMD5)
		}
	}

	return &pac, nil
}

func readArray(r io.Reader) ([]byte, error) {
	// read length (uint16)
	var length uint16
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	// read bytes
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
