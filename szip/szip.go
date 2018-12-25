package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fullsailor/pkcs7"
)

var (
	mode      string
	hash      string
	cert      string
	pkey      string
	dataPath  string
	modesEnum = []string{"z", "x", "i"}
	enc       *xml.Encoder
	metaBuf   = new(bytes.Buffer)
)

const zName = "szip"
const metaName = "meta.xml"

type metaStruct struct {
	XMLName          xml.Name  `xml:"meta"`
	Name             string    `xml:"name"`
	UncompressedSize uint64    `xml:"size>original_size"`
	ModTime          time.Time `xml:"mod_time"`
	SHA1             string    `xml:"sha1_hash"`
}

func init() {
	flag.StringVar(&mode, "mode", "required", "mode")
	flag.StringVar(&hash, "hash", "", "hash")
	flag.StringVar(&cert, "cert", "./my.crt", "certificate path")
	flag.StringVar(&pkey, "pkey", "./my.key", "private key path")
	flag.StringVar(&dataPath, "path", "./data/", "read/write files path")
}

func main() {
	flag.Parse()
	execute(mode)
}

func execute(mode string) {
	var err error
	switch mode {
	case modesEnum[0]:
		err = zipFunc(filepath.Clean(zName))
	case modesEnum[1]:
		err = extract(filepath.Clean(zName))
	case modesEnum[2]:
		err = info(filepath.Clean(zName))
	default:
		err = errors.New("mode can be only -z, -x or -i")
	}
	log.Fatal(err)
}

func addData(zPath string, w *zip.Writer) (err error) {
	data, err := os.Open(filepath.Join(dataPath, zPath))
	if err != nil {
		return
	}
	defer data.Close()
	dirinfo, err := data.Readdir(-1)
	if err != nil {
		return
	}
	for _, file := range dirinfo {
		if file.IsDir() {
			newFolder := filepath.ToSlash(filepath.Join(zPath, file.Name())) + "/"
			_, err = w.Create(newFolder)
			if err != nil {
				return
			}
			addData(newFolder, w)
		} else {
			f, err := os.Open(filepath.Join(dataPath, zPath, file.Name()))
			if err != nil {
				return err
			}
			info, err := f.Stat()
			if err != nil {
				return err
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			fpath := filepath.Join(zPath, file.Name())
			header.Name = fpath
			header.Method = zip.Deflate
			writer, err := w.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = io.Copy(writer, f)
			if err != nil {
				return err
			}
			v := &metaStruct{
				Name:             fpath,
				UncompressedSize: header.UncompressedSize64,
				ModTime:          header.ModTime(),
			}
			h := sha1.New()
			_, err = f.Seek(0, 0)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			if err != nil {
				return err
			}
			f.Close()
			v.SHA1 = fmt.Sprintf("%x", h.Sum(nil))
			err = enc.Encode(v)
			if err != nil {
				return err
			}
		}
	}
	return
}

func zipFunc(name string) (err error) {
	fz, err := os.Create(name + ".zip")
	if err != nil {
		return
	}
	w := zip.NewWriter(fz)
	enc = xml.NewEncoder(metaBuf)
	enc.Indent("  ", "    ")
	err = addData("", w)
	if err != nil {
		return
	}
	err = w.Close()
	if err != nil {
		return
	}
	fz.Close()
	err = createSZP(name)
	return
}

func createSZP(name string) (err error) {
	zname := name + ".zip"
	szpname := name + ".szp"
	meta, err := compressData(metaBuf.Bytes())
	if err != nil {
		return
	}
	fz, err := os.Open(zname)
	if err != nil {
		return
	}
	z, err := ioutil.ReadAll(fz)
	if err != nil {
		return
	}
	szp, err := os.Create(szpname)
	if err != nil {
		return
	}
	defer szp.Close()
	buf := new(bytes.Buffer)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(meta)))
	_, err = buf.Write(size)
	if err != nil {
		return
	}
	_, err = buf.Write(meta)
	if err != nil {
		return
	}
	_, err = buf.Write(z)
	if err != nil {
		return
	}
	d, err := signData(buf.Bytes(), filepath.Clean(cert), filepath.Clean(pkey))
	if err != nil {
		return
	}
	_, err = szp.Write(d)
	if err != nil {
		return
	}
	fz.Close()
	err = os.Remove(zname)
	return
}

func readSZP(data []byte) (meta []byte, z []byte, err error) {
	p, _ := pem.Decode(data)
	if p == nil {
		return nil, nil, errors.New("failed to parse PEM block")
	}
	p7, err := pkcs7.Parse(p.Bytes)
	if err != nil {
		return
	}
	data = p7.Content
	initialSize := 4
	size := data[:initialSize]
	metaEnd := initialSize + int(binary.LittleEndian.Uint32(size))
	meta = data[initialSize:metaEnd]
	meta, err = uncompressData(meta)
	if err != nil {
		return
	}
	z = data[metaEnd:]
	return
}

func extract(name string) (err error) {
	szp, err := verifySign(name + ".szp")
	if err != nil {
		return
	}
	meta, z, err := readSZP(szp)
	if err != nil {
		return
	}
	fz, err := os.Create(name + ".zip")
	if err != nil {
		return
	}
	_, err = fz.Write(z)
	if err != nil {
		return
	}
	fz.Close()
	zr, err := zip.OpenReader(name + ".zip")
	if err != nil {
		return
	}
	buf := new(bytes.Buffer)
	_, err = buf.Write(meta)
	if err != nil {
		return
	}
	dec := xml.NewDecoder(buf)
	var metaUnion []metaStruct
	for {
		var v metaStruct
		err = dec.Decode(&v)
		if err == io.EOF {
			break
		}
		metaUnion = append(metaUnion, v)
	}
	os.MkdirAll(filepath.Clean(dataPath), os.FileMode('d'))
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			h := sha1.New()
			rc, err := f.Open()
			if err != nil {
				return err
			}
			_, err = io.Copy(h, rc)
			if err != nil {
				return err
			}
			for _, v := range metaUnion {
				if !strings.EqualFold(v.Name, f.Name) {
					continue
				}
				if !strings.EqualFold(v.SHA1, fmt.Sprintf("%x", h.Sum(nil))) {
					return errors.New("Hash of " + f.Name + " does not match")
				}
				break
			}
			file, err := os.Create(filepath.Join(dataPath, f.Name))
			if err != nil {
				return err
			}
			rc, err = f.Open()
			if err != nil {
				return err
			}
			_, err = io.Copy(file, rc)
			if err != nil {
				return err
			}
			file.Close()
			rc.Close()
		} else {
			os.MkdirAll(filepath.Join(dataPath, f.Name), os.FileMode('d'))
		}
	}
	zr.Close()
	err = os.Remove(name + ".zip")
	return
}

func info(name string) (err error) {
	szp, err := verifySign(name + ".szp")
	if err != nil {
		return
	}
	meta, _, err := readSZP(szp)
	if err != nil {
		return
	}
	fmt.Printf("%s", meta)
	return
}

func getCertificate(path string) (c *x509.Certificate, err error) {
	bf, err := os.Open(path)
	if err != nil {
		return
	}
	b, err := ioutil.ReadAll(bf)
	if err != nil {
		return
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}
	c, err = x509.ParseCertificate(block.Bytes)
	return
}

func getPrivateKey(path string) (p interface{}, err error) {
	bf, err := os.Open(path)
	if err != nil {
		return
	}
	b, err := ioutil.ReadAll(bf)
	if err != nil {
		return
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}
	p, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	return
}

func signData(data []byte, cPath string, keyPath string) (sign []byte, err error) {
	c, err := getCertificate(cPath)
	if err != nil {
		return
	}
	p, err := getPrivateKey(keyPath)
	if err != nil {
		return
	}
	signedData, err := pkcs7.NewSignedData(data)
	if err != nil {
		return nil, errors.New("Cannot initialize signed data")
	}
	if err := signedData.AddSigner(c, p, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, errors.New("Cannot add signer")
	}
	sign, err = signedData.Finish()
	if err != nil {
		return nil, errors.New("Cannot finish signing data")
	}
	buf := new(bytes.Buffer)
	pem.Encode(buf, &pem.Block{Type: "PKCS7", Bytes: sign})
	return buf.Bytes(), err
}

func verifySign(name string) (data []byte, err error) {
	fszp, err := os.Open(name)
	if err != nil {
		return
	}
	defer fszp.Close()
	szp, err := ioutil.ReadAll(fszp)
	if err != nil {
		return
	}
	p, _ := pem.Decode(szp)
	if p == nil {
		return nil, errors.New("failed to parse PEM block")
	}
	p7, err := pkcs7.Parse(p.Bytes)
	if err != nil {
		return
	}
	if hash != "" {
		h := sha1.Sum(szp)
		if strings.EqualFold(fmt.Sprintf("%x", h), hash) {
			fmt.Println("Hash of the certificate matches the specified")
		} else {
			return nil, errors.New("Hash of the certificate does not match the specified")
		}
	}
	err = p7.Verify()
	if err != nil {
		return
	}
	fmt.Println("The sign has been successfully verified")
	return szp, err
}

func compressData(data []byte) (newData []byte, err error) {
	buf := new(bytes.Buffer)
	_, err = buf.Write(data)
	if err != nil {
		return
	}
	nbuf := new(bytes.Buffer)
	w, err := flate.NewWriter(nbuf, -1)
	if err != nil {
		return
	}
	_, err = w.Write(buf.Bytes())
	if err != nil {
		return
	}
	err = w.Close()
	if err != nil {
		return
	}
	return nbuf.Bytes(), err
}

func uncompressData(data []byte) (newData []byte, err error) {
	buf := new(bytes.Buffer)
	_, err = buf.Write(data)
	if err != nil {
		return
	}
	rc := flate.NewReader(buf)
	nbuf := new(bytes.Buffer)
	_, err = io.Copy(nbuf, rc)
	if err != nil {
		return
	}
	err = rc.Close()
	if err != nil {
		return
	}
	return nbuf.Bytes(), err
}
