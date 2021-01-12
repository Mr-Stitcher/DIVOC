
package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/divoc/api/config"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"time"
	"github.com/signintech/gopdf"
	"github.com/skip2/go-qrcode"
)

type Certificate struct {
	Context           []string `json:"@context"`
	Type              []string `json:"type"`
	CredentialSubject struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		Name        string `json:"name"`
		Gender      string `json:"gender"`
		Age         int    `json:"age"`
		Nationality string `json:"nationality"`
	} `json:"credentialSubject"`
	Issuer       string    `json:"issuer"`
	IssuanceDate time.Time `json:"issuanceDate"`
	Evidence     []struct {
		ID             string    `json:"id"`
		FeedbackURL    string    `json:"feedbackUrl"`
		InfoURL        string    `json:"infoUrl"`
		Type           []string  `json:"type"`
		Batch          string    `json:"batch"`
		Vaccine        string    `json:"vaccine"`
		Manufacturer   string    `json:"manufacturer"`
		Date           time.Time `json:"date"`
		EffectiveStart string    `json:"effectiveStart"`
		EffectiveUntil string    `json:"effectiveUntil"`
		Verifier       struct {
			Name string `json:"name"`
		} `json:"verifier"`
		Facility struct {
			Name    string `json:"name"`
			Address struct {
				StreetAddress  string `json:"streetAddress"`
				StreetAddress2 string `json:"streetAddress2"`
				District       string `json:"district"`
				City           string `json:"city"`
				AddressRegion  string `json:"addressRegion"`
				AddressCountry string `json:"addressCountry"`
			} `json:"address"`
		} `json:"facility"`
	} `json:"evidence"`
	NonTransferable string `json:"nonTransferable"`
	Proof           struct {
		Type               string    `json:"type"`
		Created            time.Time `json:"created"`
		VerificationMethod string    `json:"verificationMethod"`
		ProofPurpose       string    `json:"proofPurpose"`
		Jws                string    `json:"jws"`
	} `json:"proof"`
}

type PullURIRequest struct {
	XMLName    xml.Name `xml:"PullURIRequest"`
	Text       string   `xml:",chardata"`
	Ns2        string   `xml:"ns2,attr"`
	Ver        string   `xml:"ver,attr"`
	Ts         string   `xml:"ts,attr"`
	Txn        string   `xml:"txn,attr"`
	OrgId      string   `xml:"orgId,attr"`
	Format     string   `xml:"format,attr"`
	DocDetails struct {
		Text         string `xml:",chardata"`
		DocType      string `xml:"DocType"`
		DigiLockerId string `xml:"DigiLockerId"`
		UID          string `xml:"UID"`
		FullName     string `xml:"FullName"`
		DOB          string `xml:"DOB"`
		Photo        string `xml:"Photo"`
		UDF1         string `xml:"UDF1"`
		UDF2         string `xml:"UDF2"`
		UDF3         string `xml:"UDF3"`
		UDFn         string `xml:"UDFn"`
	} `xml:"DocDetails"`
}

type PullURIResponse struct {
	XMLName        xml.Name `xml:"PullURIResponse"`
	Text           string   `xml:",chardata"`
	Ns2            string   `xml:"ns2,attr"`
	ResponseStatus struct {
		Text   string `xml:",chardata"`
		Status string `xml:"Status,attr"`
		Ts     string `xml:"ts,attr"`
		Txn    string `xml:"txn,attr"`
	} `xml:"ResponseStatus"`
	DocDetails struct {
		Text         string `xml:",chardata"`
		DocType      string `xml:"DocType"`
		DigiLockerId string `xml:"DigiLockerId"`
		UID          string `xml:"UID"`
		FullName     string `xml:"FullName"`
		DOB          string `xml:"DOB"`
		UDF1         string `xml:"UDF1"`
		UDF2         string `xml:"UDF2"`
		URI          string `xml:"URI"`
		DocContent   string `xml:"DocContent"`
		DataContent  string `xml:"DataContent"`
	} `xml:"DocDetails"`
}


func ValidMAC(message, messageMAC, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)
	if log.IsLevelEnabled(log.InfoLevel) {
		log.Infof("Expected mac %s but got %s", base64.StdEncoding.EncodeToString(expectedMAC), base64.StdEncoding.EncodeToString(messageMAC))
	}
	return hmac.Equal(messageMAC, expectedMAC)
}
func uriRequest(w http.ResponseWriter, req *http.Request) {
	log.Info("Got request ")
	requestBuffer := make([]byte ,2048)
	n, _ := req.Body.Read(requestBuffer)
	log.Infof("Read %d bytes ", n)
	request := string(requestBuffer)
	log.Infof("Request body %s", request)

	hmacDigest := req.Header.Get(config.Config.Digilocker.AuthKeyName)
	hmacSignByteArray, e := base64.StdEncoding.DecodeString(hmacDigest)
	if e != nil {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("Error in verifying request signature"));
		return
	}

	if ValidMAC(requestBuffer, hmacSignByteArray, []byte(config.Config.Digilocker.AuthHMACKey)) {

		xmlRequest := PullURIRequest{}
		if err := xml.Unmarshal(requestBuffer, &xmlRequest); err != nil {
			log.Errorf("Error in marshalling request from the digilocker %+v", err)
		} else {

			response := PullURIResponse{}
			response.ResponseStatus.Ts = xmlRequest.Ts
			response.ResponseStatus.Txn = xmlRequest.Txn
			response.ResponseStatus.Status = "1"
			response.DocDetails.DocType = config.Config.Digilocker.DocType
			response.DocDetails.DigiLockerId = xmlRequest.DocDetails.DigiLockerId
			response.DocDetails.FullName = xmlRequest.DocDetails.FullName
			response.DocDetails.DOB = xmlRequest.DocDetails.DOB

			certBundle := getCertificate(xmlRequest.DocDetails.FullName, xmlRequest.DocDetails.DOB,
				xmlRequest.DocDetails.UID, xmlRequest.DocDetails.UDF1)

			response.DocDetails.URI = certBundle.Uri
			if xmlRequest.Format == "pdf" || xmlRequest.Format == "both" {
				pdfContent := certBundle.pdf // todo get pdf
				response.DocDetails.DocContent = base64.StdEncoding.EncodeToString(pdfContent)
			}
			if xmlRequest.Format == "both" || xmlRequest.Format == "xml" {
				certificateId:= certBundle.certificateId
				xmlCert := "<certificate id=\"" + certificateId + "\"><![CDATA[" + certBundle.signedJson + "]]></certificate>"
				response.DocDetails.DataContent = base64.StdEncoding.EncodeToString([]byte(xmlCert))
			}

			if responseBytes, err := xml.Marshal(response); err != nil {
				log.Errorf("Error while serializing xml")
			} else {
				w.WriteHeader(200)
				_, _ = w.Write(responseBytes)
				return
			}
			w.WriteHeader(500)
		}
	} else {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("Unauthorized"));
	}

}

type VaccinationCertificateBundle struct {
	certificateId string
	Uri string
	signedJson string
	pdf [] byte
}

func getCertificate(fullName string, dob string, aadhaar string, phoneNumber string) (* VaccinationCertificateBundle) {
	var cert VaccinationCertificateBundle
	cert.certificateId = "234234"
	cert.Uri = "https://moh.india.gov/vc/233423"
	cert.signedJson = `{"@context":["https://www.w3.org/2018/credentials/v1","https://www.who.int/2020/credentials/vaccination/v1"],"type":["VerifiableCredential","ProofOfVaccinationCredential"],"credentialSubject":{"type":"Person","id":"did:in.gov.uidai.aadhaar:2342343334","name":"Bhaya Mitra","gender":"Male","age":27,"nationality":"Indian"},"issuer":"https://nha.gov.in/","issuanceDate":"2021-01-06T08:31:25.574Z","evidence":[{"id":"https://nha.gov.in/evidence/vaccine/123","feedbackUrl":"https://divoc.xiv.in/feedback/123","infoUrl":"https://divoc.xiv.in/learn/123","type":["Vaccination"],"batch":"MB3428BX","vaccine":"CoVax","manufacturer":"COVPharma","date":"2020-12-02T19:21:18.646Z","effectiveStart":"2020-12-02","effectiveUntil":"2025-12-02","verifier":{"name":"Sooraj Singh"},"facility":{"name":"ABC Medical Center","address":{"streetAddress":"123, Koramangala","streetAddress2":"","district":"Bengaluru South","city":"Bengaluru","addressRegion":"Karnataka","addressCountry":"IN"}}}],"nonTransferable":"true","proof":{"type":"Ed25519Signature2018","created":"2021-01-10T14:43:59Z","verificationMethod":"did:example:123456#key1","proofPurpose":"assertionMethod","jws":"eyJhbGciOiJFZERTQSIsImI2NCI6ZmFsc2UsImNyaXQiOlsiYjY0Il19..xmNN4m4okKtHumXcpHe3L8PNGg5q5VBul49NwhBYOo1z_lKMlGCRDdhmLaD5Rs1mBfPvSet5qBfYW2T3UhBgAw"}}`
	if pdfBytes, err := getCertificateAsPdf(cert.signedJson); err != nil {
		log.Errorf("Error in creating certificate pdf")
	} else {
		cert.pdf = pdfBytes
	}
	return &cert
}

func getCertificateAsPdf(certificateText string) ([]byte, error) {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{ PageSize: *gopdf.PageSizeA4 })
	pdf.AddPage()
	err := pdf.AddTTFFont("wts11", "./Roboto-Medium.ttf")
	if err != nil {
		log.Print(err.Error())
		return nil, err
	}
	​
	tpl1 := pdf.ImportPage("template.pdf", 1, "/MediaBox")
	​
	// Draw pdf onto page
	pdf.UseImportedTemplate(tpl1, 0, 0, 900, 0)
	​
	err = pdf.SetFont("wts11", "", 14)
	if err != nil {
		log.Print(err.Error())
		return nil, err
	}
	qrCode, err := qrcode.New(certificateText, qrcode.Medium)
	imageBytes, err := qrCode.PNG(-3)
	holder, err := gopdf.ImageHolderByBytes(imageBytes)
	pdf.ImageByHolder(holder, 320,130,nil)
	var certificate Certificate
	if err := json.Unmarshal([]byte(certificateText), &certificate); err != nil {
		fmt.Println(err)
	}
	const offsetX = 350
	const offsetY = 440
	pdf.SetX(offsetX)
	pdf.SetY(offsetY)
	pdf.Cell(nil,certificate.IssuanceDate.Format("2006-01-02"))
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +69)
	pdf.Cell(nil,certificate.CredentialSubject.Name)
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +105)
	pdf.Cell(nil,certificate.CredentialSubject.Gender)
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +130)
	pdf.Cell(nil,strconv.Itoa(certificate.CredentialSubject.Age))
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +158)
	pdf.Cell(nil,certificate.Evidence[0].Facility.Name)
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +198)
	pdf.Cell(nil,certificate.Evidence[0].Manufacturer)
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +255)
	pdf.Cell(nil,certificate.Evidence[0].Date.Format("2006-01-02"))
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +280)
	pdf.Cell(nil,strconv.Itoa(1))
	pdf.SetX(offsetX)
	pdf.SetY(offsetY +300)
	pdf.Cell(nil,certificate.Evidence[0].EffectiveUntil)
	var b bytes.Buffer
	pdf.Write(&b)
	return b.Bytes(), nil
}

func main(){
	config.Initialize()
	log.Info("Running digilocker support api")
	http.HandleFunc("/pullUriRequest", uriRequest)
	_ = http.ListenAndServe(":8003", nil)
}