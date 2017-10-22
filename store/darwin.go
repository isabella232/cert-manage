// +build darwin

package store

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/adamdecaf/cert-manage/tools/_x509"
	"github.com/adamdecaf/cert-manage/tools/file"
	"github.com/adamdecaf/cert-manage/tools/pem"
	"github.com/adamdecaf/cert-manage/whitelist"
)

// Docs
// - https://developer.apple.com/legacy/library/documentation/Darwin/Reference/ManPages/man1/security.1.html
// - https://github.com/adamdecaf/cert-manage/issues/9#issuecomment-337778241

var (
	plistModDateFormat = "2006-01-02T15:04:05Z"
	systemDirs         = []string{
		"/System/Library/Keychains/SystemRootCertificates.keychain",
		"/Library/Keychains/System.keychain",
	}

	// internal options
	debug = strings.Contains(os.Getenv("GODEBUG"), "x509roots=1")
)

const (
	backupDirPerms = 0744
)

func getUserDirs() ([]string, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}

	return []string{
		filepath.Join(u.HomeDir, "/Library/Keychains/login.keychain"),
		filepath.Join(u.HomeDir, "/Library/Keychains/login.keychain-db"),
	}, nil
}

func getCertManageDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, "/Library/cert-manage"), nil
}

func getLatestBackupFile() (string, error) {
	dir, err := getCertManageDir()
	if err != nil {
		return "", err
	}
	fis, err := ioutil.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(fis) == 0 {
		return "", nil
	}

	// get largest
	file.SortFileInfos(fis)
	latest := fis[len(fis)-1]
	return filepath.Join(dir, latest.Name()), nil
}

type darwinStore struct{}

func platform() Store {
	return darwinStore{}
}

// Backup will save off a copy of the existing trust policy
func (s darwinStore) Backup() error {
	fd, err := trustSettingsExport()
	defer os.Remove(fd.Name())
	if err != nil {
		return err
	}

	// Copy the temp file somewhere safer
	outDir, err := getCertManageDir()
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("trust-backup-%d.xml", time.Now().Unix())
	out := filepath.Join(outDir, filename)

	// Copy file
	err = os.MkdirAll(outDir, backupDirPerms)
	if err != nil {
		return err
	}
	err = file.CopyFile(fd.Name(), out)
	return err
}

// List
//
// Note: Currently we are ignoring the login keychain. This is done because those certs are
// typically modified by the user (or an application the user trusts).
func (s darwinStore) List() ([]*x509.Certificate, error) {
	installed, err := readInstalledCerts(systemDirs...)
	if err != nil {
		return nil, err
	}
	trustItems, err := getCertsWithTrustPolicy()
	if err != nil {
		return nil, err
	}

	if debug {
		fmt.Printf("%d installed, %d with policy\n", len(installed), len(trustItems))
	}

	kept := make([]*x509.Certificate, 0)
	for i := range installed {
		if installed[i] == nil {
			continue
		}
		if trustItems.contains(installed[i]) {
			kept = append(kept, installed[i])
			continue
		}
	}

	return kept, nil
}

// readInstalledCerts pulls certificates from the `security` cli tool that's
// installed. This will return certificates, but not their trust status.
func readInstalledCerts(paths ...string) ([]*x509.Certificate, error) {
	res := make([]*x509.Certificate, 0)

	args := []string{"find-certificate", "-a", "-p"}
	args = append(args, paths...)

	b, err := exec.Command("/usr/bin/security", args...).Output()
	if err != nil {
		return nil, err
	}

	cs, err := pem.Parse(b)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if c == nil {
			continue
		}
		add := true
		for i := range res {
			if reflect.DeepEqual(c.Signature, res[i].Signature) {
				add = false
				break
			}
		}
		if add {
			res = append(res, c)
		}
	}

	return res, nil
}

func getCertsWithTrustPolicy() (trustItems, error) {
	fd, err := trustSettingsExport()
	defer os.Remove(fd.Name())
	if err != nil {
		return nil, err
	}

	plist, err := parsePlist(fd)
	if err != nil {
		return nil, err
	}

	return plist.convertToTrustItems(), nil
}

// returns an os.File for the plist file written
// Note: Callers are expected to cleanup the file handler
func trustSettingsExport(args ...string) (*os.File, error) {
	// Create temp file for plist output
	fd, err := ioutil.TempFile("", "trust-settings")
	if err != nil {
		return nil, err
	}

	// build up args
	args = append([]string{
		"trust-settings-export", "-s", fd.Name(),
	}, args...)

	// run command
	_, err = exec.Command("/usr/bin/security", args...).Output()
	if err != nil {
		return nil, err
	}

	return fd, nil
}

func (s darwinStore) Remove(wh whitelist.Whitelist) error {
	certs, err := s.List()
	if err != nil {
		return err
	}

	// Keep what's whitelisted
	kept := make([]*x509.Certificate, 0)
	for i := range certs {
		if wh.Matches(certs[i]) {
			kept = append(kept, certs[i])
		}
	}

	// Build plist xml file and restore on the system
	trustItems := make(trustItems, 0)
	for i := range kept {
		if kept[i] == nil {
			continue
		}
		trustItems = append(trustItems, trustItemFromCertificate(*kept[i]))
	}

	// Create temporary output file
	f, err := ioutil.TempFile("", "cert-manage")
	// defer os.Remove(f.Name())
	if err != nil {
		return err
	}

	// Write out plist file
	err = trustItems.toPlist().toXmlFile(f.Name())
	if err != nil {
		return err
	}

	fmt.Println(f.Name())
	return nil

	// return s.Restore(f.Name())
}

func (s darwinStore) Restore(where string) error {
	// Setup file to use as restore point
	if where == "" {
		// Ignore any errors and try to set a file
		latest, _ := getLatestBackupFile()
		where = latest
	}
	if where == "" {
		// No backup dir (or backup files) and no -file specified
		return errors.New("No backup file found and -file not specified")
	}
	if !file.Exists(where) {
		return errors.New("Restore file doesn't exist")
	}

	// run restore
	args := []string{"trust-settings-import", "-d", where}
	_, err := exec.Command("/usr/bin/security", args...).Output()

	return err
}

type trustItems []trustItem

func (t trustItems) contains(cert *x509.Certificate) bool {
	if cert == nil {
		// we don't want to say we've got a nil cert
		return true
	}
	fp := _x509.GetHexSHA1Fingerprint(*cert)
	for i := range t {
		if fp == t[i].sha1Fingerprint {
			return true
		}
	}
	return false
}

func (t trustItems) toPlist() plist {
	out := plist{}
	// Set defaults
	out.ChiDict = &chiDict{ChiDict: &chiDict{ChiDict: &chiDict{}}}
	out.ChiDict.ChiKey = make([]*chiKey, 1)
	out.ChiDict.ChiKey[0] = &chiKey{Text: "trustList"}

	// TODO(adam): Need to add ?
	// <key>trustVersion</key>
        // <integer>1</integer>

	// Add each cert, the reverse of `chiPlist.convertToTrustItems()`
	keys := make([]*chiKey, len(t))
	dates := make([]*chiDate, len(t))
	max := len(t) * 2
	data := make([]*chiData, max) // twice as many <data></data> elements
	for i := 0; i < max; i += 2 {
		keys[i/2] = &chiKey{Text: strings.ToUpper(t[i/2].sha1Fingerprint)}

		// issuer
		rdn := t[i/2].issuerName.ToRDNSequence()
		bs, _ := asn1.Marshal(&rdn)
		data[i/2] = &chiData{Text: base64.StdEncoding.EncodeToString(bs)}

		// modDate
		dates[i/2] = &chiDate{Text: t[i/2].modDate.Format(plistModDateFormat)}

		// serial number
		data[i] = &chiData{Text: base64.StdEncoding.EncodeToString(t[i/2].serialNumber)}
	}

	// Build the final result
	out.ChiDict.ChiDict.ChiDict.ChiData = data
	out.ChiDict.ChiDict.ChiDict.ChiDate = dates
	out.ChiDict.ChiDict.ChiKey = keys

	return out
}

// trustItem represents an entry from the plist (xml) files produced by
// the /usr/bin/security cli tool
type trustItem struct {
	// required
	sha1Fingerprint string
	issuerName      pkix.Name
	modDate         time.Time
	serialNumber    []byte

	// optional
	// TODO(adam): needs picked up?
	kSecTrustSettingsResult int32
}

func trustItemFromCertificate(cert x509.Certificate) trustItem {
	return trustItem{
		sha1Fingerprint: _x509.GetHexSHA1Fingerprint(cert),
		issuerName:      cert.Issuer,
		modDate:         time.Now(),
		serialNumber:    cert.SerialNumber.Bytes(),
	}
}

func (t trustItem) Serial() *big.Int {
	serial := big.NewInt(0)
	serial.SetBytes(t.serialNumber)
	return serial
}

func (t trustItem) String() string {
	modDate := t.modDate.Format(plistModDateFormat)

	name := fmt.Sprintf("O=%s", strings.Join(t.issuerName.Organization, " "))
	if t.issuerName.CommonName != "" {
		name = fmt.Sprintf("CN=%s", t.issuerName.CommonName)
	}

	country := strings.Join(t.issuerName.Country, " ")

	return fmt.Sprintf("SHA1 Fingerprint: %s\n %s (%s)\n modDate: %s\n serialNumber: %d", t.sha1Fingerprint, name, country, modDate, t.Serial())
}
