package main

import "os"

func LoadPACFromFile(path string) (*PAC, error) {
	var err error

	data, err := os.ReadFile(path)
	pac, err := LoadPAC(data)

	if err != nil {
		return nil, err
	}

	return pac, nil
}
