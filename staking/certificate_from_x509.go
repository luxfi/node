// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import "crypto/x509"

// CertificateFromX509 converts an x509.Certificate to a staking.Certificate
func CertificateFromX509(cert *x509.Certificate) *Certificate {
	if cert == nil {
		return nil
	}
	return &Certificate{
		Raw:       cert.Raw,
		PublicKey: cert.PublicKey,
	}
}