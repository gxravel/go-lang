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
	"log"
	"os"
	"time"

	"github.com/fullsailor/pkcs7"
)

var mode string
var hash string
var cert string
var pkey string
var path string
var modesEnum = []string{"z", "x", "i"}

const zName = "szip"

type Meta struct {
	XMLName          xml.Name  `xml:"meta"`
	Name             string    `xml:"name"`
	UncompressedSize uint64    `xml:"size>original_size"`
	CompressedSize   uint64    `xml:"size>compressed_size"`
	ModTime          time.Time `xml:"mod_time"`
	SHA1             string    `xml:"sha1_hash"`
}

func init() {
	flag.StringVar(&mode, "mode", "required", "mode")
	flag.StringVar(&hash, "hash", "", "hash")
	flag.StringVar(&cert, "cert", "./data2/my.crt", "certificate path")
	flag.StringVar(&pkey, "pkey", "./data2/my.key", "private key path")
	flag.StringVar(&path, "path", "./data/", "read/write files path")
}

func Execute(mode string) {
	switch mode {
	case modesEnum[0]:
		Zip(zName)
	case modesEnum[1]:
		Extract(zName)
	case modesEnum[2]:
		Info(zName)
	default:
		fmt.Print("mode can be only -z, -x or -i")
	}
}

func CheckErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func OpenFile(path string) *os.File {
	f, err := os.Open(path)
	CheckErr(err)
	return f
}

func ReadFile(path string) ([]byte, int) {
	f := OpenFile(path)
	defer f.Close()
	fi, err := f.Stat()
	CheckErr(err)
	b := make([]byte, fi.Size())
	count, err := f.Read(b)
	CheckErr(err)
	return b, count
}

func CreateFile(path string) *os.File {
	w, err := os.Create(path)
	CheckErr(err)
	return w
}

func AddData(zPath string, w *zip.Writer) {
	data := OpenFile(path + zPath)
	defer data.Close()
	dirInfo, dErr := data.Readdir(-1)
	CheckErr(dErr)
	for _, file := range dirInfo {
		if file.IsDir() {
			newFolder := zPath + file.Name() + "/"
			_, err := w.Create(newFolder)
			CheckErr(err)
			AddData(newFolder, w)
		} else {
			f := OpenFile(path + zPath + file.Name())
			defer f.Close()
			info, err := f.Stat()
			CheckErr(err)
			header, err := zip.FileInfoHeader(info)
			CheckErr(err)
			header.Name = zPath + file.Name()
			header.Method = zip.Deflate
			writer, err := w.CreateHeader(header)
			CheckErr(err)
			_, err = io.Copy(writer, f)
			CheckErr(err)
		}
	}
}

func Zip(name string) {
	fz := CreateFile(name + ".zip")
	w := zip.NewWriter(fz)
	AddData("", w)
	err := w.Close()
	CheckErr(err)
	fz.Close()

	CreateSZP(name)
}

func CreateSZP(name string) {
	r, err := zip.OpenReader(name + ".zip")
	CheckErr(err)
	buf := new(bytes.Buffer)
	enc := xml.NewEncoder(buf)
	enc.Indent("  ", "    ")
	for _, val := range r.File {
		if val.FileInfo().IsDir() {
			continue
		}
		v := &Meta{
			Name:             path + val.Name,
			UncompressedSize: val.UncompressedSize64,
			CompressedSize:   val.CompressedSize64,
			ModTime:          val.ModTime()}
		h := sha1.New()
		f, err := val.Open()
		CheckErr(err)
		_, err = io.Copy(h, f)
		CheckErr(err)
		v.SHA1 = fmt.Sprintf("%x", h.Sum(nil))
		err = enc.Encode(v)
		CheckErr(err)
		f.Close()
	}
	r.Close()
	meta, metaSize := CompressData(buf.Bytes())
	rf, _ := ReadFile(name + ".zip")
	crt, crtSize := SignData(rf, cert, pkey)
	szp := CreateFile(name + ".szp")
	defer szp.Close()
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(crtSize))
	_, err = szp.Write(size)
	CheckErr(err)
	_, err = szp.Write(crt)
	CheckErr(err)
	binary.LittleEndian.PutUint32(size, uint32(metaSize))
	_, err = szp.Write(size)
	CheckErr(err)
	_, err = szp.Write(meta)
	CheckErr(err)
	_, err = szp.Write(rf)
	CheckErr(err)
	err = os.Remove(name + ".zip")
	CheckErr(err)
}

func ReadSZP(name string) (crt []byte, meta []byte, z []byte) {
	szp := OpenFile(name + ".szp")
	defer szp.Close()
	size := make([]byte, 4)
	_, err := szp.Read(size)
	CheckErr(err)
	crt = make([]byte, binary.LittleEndian.Uint32(size))
	_, err = szp.Read(crt)
	CheckErr(err)

	_, err = szp.Read(size)
	CheckErr(err)
	meta = make([]byte, binary.LittleEndian.Uint32(size))
	_, err = szp.Read(meta)
	CheckErr(err)
	meta, _ = UncompressData(meta)

	szpi, err := szp.Stat()
	CheckErr(err)
	z = make([]byte, szpi.Size())
	zSize, err := szp.Read(z)
	CheckErr(err)
	z = z[:zSize]
	return crt, meta, z
}

func Extract(name string) {
	crt, meta, z := ReadSZP(name)
	err := VerifySign(z, crt)
	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Println("The sign has been successfully verified")
	}
	fz := CreateFile(name + ".zip")
	_, err = fz.Write(z)
	CheckErr(err)
	fz.Close()
	zr, err := zip.OpenReader(name + ".zip")
	CheckErr(err)
	os.Mkdir(path, os.FileMode('d'))
	for _, f := range zr.File {
		rc, err := f.Open()
		CheckErr(err)
		defer rc.Close()
		if !f.FileInfo().IsDir() {
			h := sha1.New()
			_, err = io.Copy(h, rc)
			CheckErr(err)
			i := bytes.Index(meta, []byte(f.Name))
			j := bytes.Index(meta[i:], []byte("</meta>"))
			b := bytes.Contains(meta[i:i+j], []byte(fmt.Sprintf("%x", h.Sum(nil))))
			if !b {
				log.Fatal(errors.New("Hash of " + f.Name + " does not match"))
			}
			file, err := os.Create(path + f.Name)
			CheckErr(err)
			defer file.Close()
			rc.Close()
			rc, err := f.Open()
			CheckErr(err)
			_, err = io.CopyN(file, rc, int64(f.UncompressedSize64))
			CheckErr(err)
		} else {
			err = os.Mkdir(path+f.Name, os.FileMode('d'))
		}
	}
	zr.Close()
	err = os.Remove("szip.zip")
	CheckErr(err)
}

func Info(name string) {
	crt, meta, z := ReadSZP(name)
	err := VerifySign(z, crt)
	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Println("The sign has been successfully verified")
	}
	fmt.Println(string(meta))
}

func SignData(data []byte, cPath string, kPath string) (newData []byte, size int) {
	b, _ := ReadFile(cPath)
	block, _ := pem.Decode(b)
	if block == nil {
		panic("failed to parse PEM block")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	CheckErr(err)
	b, _ = ReadFile(kPath)
	block, _ = pem.Decode(b)
	if block == nil {
		panic("failed to parse PEM block")
	}
	p, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	CheckErr(err)
	// Initialize a SignedData struct with content to be signed
	signedData, err := pkcs7.NewSignedData(data)
	if err != nil {
		fmt.Printf("Cannot initialize signed data: %s", err)
	}
	// Add the signing cert and private key
	if err := signedData.AddSigner(c, p, pkcs7.SignerInfoConfig{}); err != nil {
		fmt.Printf("Cannot add signer: %s", err)
	}
	signedData.Detach()

	// Finish() to obtain the signature bytes
	detachedSignature, err := signedData.Finish()
	if err != nil {
		fmt.Printf("Cannot finish signing data: %s", err)
	}
	buf := new(bytes.Buffer)
	pem.Encode(buf, &pem.Block{Type: "PKCS7", Bytes: detachedSignature})
	return buf.Bytes(), buf.Len()
}

func VerifySign(data []byte, pkcs []byte) error {
	p, _ := pem.Decode(pkcs)
	p7, err := pkcs7.Parse(p.Bytes)
	CheckErr(err)
	p7.Content = data
	if hash != "" {
		h := sha1.Sum(pkcs)
		if fmt.Sprintf("%x", h) == hash {
			fmt.Println("Hash of the cetrificate matches the specified")
		} else {
			log.Fatal(errors.New("Hash of the cetrificate does not match the specified"))
		}
	}
	return p7.Verify()
}

func CompressData(data []byte) (newData []byte, size int) {
	buf := new(bytes.Buffer)
	_, err := buf.Write(data)
	CheckErr(err)
	nbuf := new(bytes.Buffer)
	w, err := flate.NewWriter(nbuf, -1)
	CheckErr(err)
	_, err = w.Write(buf.Bytes())
	CheckErr(err)
	err = w.Close()
	CheckErr(err)
	return nbuf.Bytes(), nbuf.Len()
}

func UncompressData(data []byte) (newData []byte, size int) {
	buf := new(bytes.Buffer)
	_, err := buf.Write(data)
	CheckErr(err)
	rc := flate.NewReader(buf)
	d := make([]byte, 1024)
	nbuf := new(bytes.Buffer)
	for {
		n, err := rc.Read(d)
		if err == io.EOF {
			_, err2 := nbuf.Write(d[:n])
			CheckErr(err2)
			break
		}
		_, err = nbuf.Write(d)
		CheckErr(err)
	}
	err = rc.Close()
	CheckErr(err)
	return nbuf.Bytes(), nbuf.Len()
}

func main() {
	flag.Parse()
	Execute(mode)
}
