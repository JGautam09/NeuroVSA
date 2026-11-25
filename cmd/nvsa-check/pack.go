package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JGautam09/NeuroVSA/internal/checkselect"
)

func packFlags(fs *flag.FlagSet) (trust, bundle *string) {
	return fs.String("trust", "", "local trust file for optional pack suggestions"),
		fs.String("bundle", "", "merged signed pack bundle")
}

func loadSnapshot(config, trustFile, bundleFile string) (checkselect.Snapshot, error) {
	policy, err := checkselect.LoadPolicy(config)
	if err != nil {
		return checkselect.Snapshot{}, err
	}
	s := checkselect.Snapshot{Version: 1, Policy: policy}
	if (trustFile == "") != (bundleFile == "") {
		return s, fmt.Errorf("-trust and -bundle must be supplied together")
	}
	if trustFile != "" {
		trust, err := checkselect.ReadJSON[checkselect.Trust](trustFile)
		if err != nil {
			return s, err
		}
		bundle, err := checkselect.ReadJSON[checkselect.Bundle](bundleFile)
		if err != nil {
			return s, err
		}
		s.Trust, s.Bundle = &trust, &bundle
	}
	return checkselect.PrepareSnapshot(s)
}

// nvsa-pack keygen already exports this key-backup format.
func signPackFile(payloadFile, keyFile string) (checkselect.SignedPack, error) {
	if payloadFile == "" || keyFile == "" {
		return checkselect.SignedPack{}, fmt.Errorf("-payload and -key are required")
	}
	payload, err := checkselect.ReadJSON[checkselect.PackPayload](payloadFile)
	if err != nil {
		return checkselect.SignedPack{}, err
	}
	key, err := checkselect.ReadJSON[struct {
		Version int    `json:"rulegarden_key"`
		Seed    []byte `json:"seed_b64"`
	}](keyFile)
	if err != nil || key.Version != 1 || len(key.Seed) != ed25519.SeedSize {
		return checkselect.SignedPack{}, fmt.Errorf("invalid nvsa-pack key backup")
	}
	return checkselect.SignPack(payload, ed25519.NewKeyFromSeed(key.Seed))
}

type fileFlags []string

func (f *fileFlags) String() string { return strings.Join(*f, ", ") }
func (f *fileFlags) Set(value string) error {
	if value == "" || len(*f) >= 256 {
		return fmt.Errorf("supply 1–256 nonempty input filenames")
	}
	*f = append(*f, value)
	return nil
}

func mergePackFiles(trustPath string, files []string) (checkselect.Bundle, error) {
	if trustPath == "" || len(files) == 0 {
		return checkselect.Bundle{}, fmt.Errorf("-trust and at least one -input are required")
	}
	trust, err := checkselect.ReadJSON[checkselect.Trust](trustPath)
	if err != nil {
		return checkselect.Bundle{}, err
	}
	bundle := checkselect.Bundle{Version: 1}
	for i, file := range files {
		in, err := checkselect.ReadJSON[struct {
			Version int                      `json:"version"`
			Packs   []checkselect.SignedPack `json:"packs"`
			checkselect.SignedPack
		}](file)
		if err != nil {
			return checkselect.Bundle{}, err
		}
		var next checkselect.Bundle
		switch {
		case in.Packs != nil && in.Payload.Version == 0 && len(in.PublicKey) == 0 && len(in.Signature) == 0:
			next = checkselect.Bundle{Version: in.Version, Packs: in.Packs}
		case in.Version == 0 && in.Packs == nil && in.Payload.Version == 1:
			next = checkselect.Bundle{Version: 1, Packs: []checkselect.SignedPack{in.SignedPack}}
		default:
			return checkselect.Bundle{}, fmt.Errorf("%s is neither a signed pack nor a bundle", file)
		}
		bundle, err = checkselect.MergeBundles(trust, bundle, next)
		if err != nil {
			return checkselect.Bundle{}, err
		}
		if i+1 < len(files) {
			if err := checkselect.WriteArtifact(io.Discard, bundle); err != nil {
				return checkselect.Bundle{}, err
			}
		}
	}
	return bundle, nil
}
