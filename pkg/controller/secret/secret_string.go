package secret

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

type StringGenerator struct {
	log logr.Logger
}
const byteSuffix = "b"

func (pg StringGenerator) generateData(instance *corev1.Secret) (reconcile.Result, error) {
	toGenerate := instance.Annotations[AnnotationSecretAutoGenerate]

	genKeys := strings.Split(toGenerate, ",")

	if err := ensureUniqueness(genKeys); err != nil {
		return reconcile.Result{}, err
	}

	var regenKeys []string
	if _, ok := instance.Annotations[AnnotationSecretSecure]; !ok && regenerateInsecure() {
		pg.log.Info("instance was generated by a cryptographically insecure PRNG")
		regenKeys = genKeys // regenerate all keys
	} else if regenerate, ok := instance.Annotations[AnnotationSecretRegenerate]; ok {
		pg.log.Info("removing regenerate annotation from instance")
		delete(instance.Annotations, AnnotationSecretRegenerate)

		if regenerate == "yes" {
			regenKeys = genKeys
		} else {
			regenKeys = strings.Split(regenerate, ",") // regenerate requested keys
		}
	}

	length, byteLen, err := secretLengthFromAnnotation(secretLength(), instance.Annotations)
	if err != nil {
		return reconcile.Result{}, err
	}


	encoding, err := secretEncodingFromAnnotation(secretEncoding(), instance.Annotations)
	if err != nil {
		return reconcile.Result{}, err
	}

	generatedCount := 0
	for _, key := range genKeys {
		if len(instance.Data[key]) != 0 && !contains(regenKeys, key) {
			// dont generate key if it already has a value
			// and is not queued for regeneration
			continue
		}
		generatedCount++

		//If B suffix was used for length annotation,
		//only use length for input byte sequence
		//and not to slice output string
			if encoding == "raw"{
			value, err := generateRandomStringRaw(length)
			if err != nil {
				pg.log.Error(err, "could not generate new random string")
				return reconcile.Result{RequeueAfter: time.Second * 30}, err
			}
			instance.Data[key] = []byte(value)

			pg.log.Info("set field of instance to new randomly generated instance", "bytes", len(value), "field", key, "encoding",encoding)
		} else {
			value, err := generateRandomString(length, encoding, byteLen)
			if err != nil {
				pg.log.Error(err, "could not generate new random string")
				return reconcile.Result{RequeueAfter: time.Second * 30}, err
			}
			instance.Data[key] = []byte(value)

			pg.log.Info("set field of instance to new randomly generated instance", "bytes", len(value), "field", key, "encoding",encoding)
		}

	}
	pg.log.Info("generated secrets", "count", generatedCount)

	if generatedCount == len(genKeys) {
		// all keys have been generated by this instance
		instance.Annotations[AnnotationSecretSecure] = "yes"
	}

	return reconcile.Result{}, nil
}

func generateRandomString(length int, encoding string, lenBytes bool) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	var encodedString string
	switch encoding {
	case "base64url":
		encodedString = base64.URLEncoding.EncodeToString(b)
	case "base32":
		encodedString = base32.StdEncoding.EncodeToString(b)
	case "hex":
		encodedString = hex.EncodeToString(b)
	default:
		encodedString = base64.StdEncoding.EncodeToString(b)
	}
	if lenBytes {
		return encodedString, nil
	} else {
		return encodedString[0:length], nil
	}

}

func generateRandomStringRaw(length int) ([]byte, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {

		return make([]byte, 0), err
	}
	return b, nil

}

// ensure elements in input array are unique
func ensureUniqueness(a []string) error {
	set := map[string]bool{}
	for _, e := range a {
		if set[e] {
			return fmt.Errorf("duplicate element %s found", e)
		}
		set[e] = true
	}
	return nil
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
