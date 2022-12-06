package util

import (
	"crypto/ecdsa"

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
// The signature must be in the format returned by crypto.Sign()
func RecoverAddressFromSignature(msg []byte, sig []byte) (*common.Address, error) {
	if len(sig) != crypto.SignatureLength {
		return nil, secp256k1.ErrInvalidSignatureLen
	}
	// According to the Ethereum Yellow Paper, the signature format must be
	// [R || S || V], and V (the Recovery ID) must be 27 or 28. This was apparently
	// inherited from Bitcoin.
	// Internally, V is 0 or 1, so we subtract 27 to get the actual recovery ID.
	// References:
	// - https://github.com/ethereum/go-ethereum/issues/19751#issuecomment-504900739
	// - https://ethereum.github.io/yellowpaper/paper.pdf, page 22, Appendix E., (213)
	sig[crypto.RecoveryIDOffset] -= 27

	// Recover the public key from the signature.
	hash := accounts.TextHash(msg)
	pubKey, err := crypto.SigToPub(hash, sig)

	// Restore V to its original value.
	sig[crypto.RecoveryIDOffset] += 27

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
// The signature is in the format returned by crypto.Sign().
func (w *Wallet) Sign(msg []byte) ([]byte, error) {
	sig, err := crypto.Sign(accounts.TextHash(msg), w.Key)
	if err != nil {
		return nil, err
	}

	// Change V from 0/1 to 27/28 to match the Ethereum Yellow Paper.
	// See RecoverAddressFromSignature() for more details.
	sig[crypto.RecoveryIDOffset] += 27
	return sig, nil
}
