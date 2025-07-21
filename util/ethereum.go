package util

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

type Wallet struct {
	Address *common.Address
	Key     *ecdsa.PrivateKey
}

// Recovers the address of the signer from a message and signature.
// Expects a signature in the format returned by eth_sign:
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_sign
// This is the format currently used to sign messages by the Rocket Pool smartnode stack:
// https://github.com/rocket-pool/smartnode/blob/9ded8d070bdd81798813e16b53657f600bab781e/shared/services/wallet/wallet.go#L305
func RecoverAddressFromSignature(msg []byte, sig []byte) (*common.Address, error) {
	if len(sig) != crypto.SignatureLength {
		return nil, secp256k1.ErrInvalidSignatureLen
	}

	v := &sig[crypto.RecoveryIDOffset]

	// According to the Ethereum Yellow Paper, the signature format must be
	// [R || S || V], and V (the Recovery ID) must be 27 or 28. This was apparently
	// inherited from Bitcoin.
	// Internally, V is 0 or 1, so we subtract 27 to get the actual recovery ID.
	// References:
	// - https://github.com/ethereum/go-ethereum/issues/19751#issuecomment-504900739
	// - https://ethereum.github.io/yellowpaper/paper.pdf, page 22, Appendix E., (213)
	//
	// Additionally, we've seen some wallets produce signatures with V=0 or 1.
	// In those cases, we don't need to tweak the recovery ID before recovering the public key.
	switch *v {
	case 27, 28:
		// Subtract 27 to get the actual recovery ID.
		*v -= 27
		// Restore V to its original value at the end of the function.
		defer func() {
			*v += 27
		}()
	case 0, 1:
		// Do nothing.
	default:
		return nil, fmt.Errorf("invalid recovery ID: %d", *v)
	}

	// Recover the public key from the signature.
	hash := accounts.TextHash(msg)
	pubKey, err := crypto.SigToPub(hash, sig)

	if err != nil {
		return nil, err
	}
	address := crypto.PubkeyToAddress(*pubKey)
	return &address, nil
}

// Generates a new wallet with a random private key.
func NewWallet() (*Wallet, error) {
	key, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	return &Wallet{
		Address: &address,
		Key:     key,
	}, nil
}

// Signs a message with the wallet's private key.
// Return a signature in the format used by eth_sign.
// See RecoverAddressFromSignature() for more details.
func (w *Wallet) Sign(msg []byte) ([]byte, error) {
	sig, err := crypto.Sign(accounts.TextHash(msg), w.Key)
	if err != nil {
		return nil, err
	}

	// Change V from 0/1 to 27/28 to match the Ethereum Yellow Paper.
	sig[crypto.RecoveryIDOffset] += 27
	return sig, nil
}
