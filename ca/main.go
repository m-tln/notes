package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"
)

func main() {
	os.MkdirAll("/certs", 0755)

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Notes Service Mesh CA"},
			CommonName:   "notes-ca",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, 
		&caKey.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}

	caCertFile, _ := os.Create("/certs/ca.crt")
	pem.Encode(caCertFile, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertDER,
	})
	caCertFile.Close()

	caKeyFile, _ := os.Create("/certs/ca.key")
	pem.Encode(caKeyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caKey),
	})
	caKeyFile.Close()

	services := map[string][]string{
		"app1":          {"app1-sidecar", "app1.notes.internal", "app1-sidecar.notes.internal"},
		"app2":          {"app2-sidecar", "app2.notes.internal", "app2-sidecar.notes.internal"},
		"app3":          {"app3-sidecar", "app3.notes.internal", "app3-sidecar.notes.internal"},
		"email":         {"email-sidecar", "email.notes.internal", "email-sidecar.notes.internal"},
		"loadbalancer":  {"loadbalancer.notes.internal"},
	}

	for service, altNames := range services {
		generateCertWithSAN(service, altNames, &caTemplate, caKey)
	}

	log.Println("All certificates with SAN generated successfully")
}

func generateCertWithSAN(service string, dnsNames []string, caTemplate *x509.Certificate, caKey *rsa.PrivateKey) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}

	allDNSNames := append([]string{service}, dnsNames...)
	
	allDNSNames = append(allDNSNames, 
		service+".notes_network",
		strings.Replace(service, "-sidecar", "", 1),
	)

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   service,
			Organization: []string{"Notes Service Mesh"},
		},
		DNSNames:    allDNSNames,
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, caTemplate, 
		&key.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}

	certFile, _ := os.Create(fmt.Sprintf("/certs/%s.crt", service))
	pem.Encode(certFile, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	certFile.Close()

	keyFile, _ := os.Create(fmt.Sprintf("/certs/%s.key", service))
	pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	keyFile.Close()

	log.Printf("Generated certificate for %s with SAN: %v", service, allDNSNames)
}