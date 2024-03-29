package auction

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// To be sent over wire, see codec below
type SignedBid struct {
	AmountWei *big.Int       `json:"amountWei"`
	L1Block   *big.Int       `json:"l1Block"`
	Address   common.Address `json:"address"`
	Signature hexutil.Bytes  `json:"signature"`
}

// To be used by relay account to sign bid for a certain amount and l1Block
func CreateSignedBid(amountWei *big.Int, l1Block *big.Int, privateKey *ecdsa.PrivateKey) (*SignedBid, error) {
	hash := getDataHash(amountWei, l1Block)
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return nil, err
	}
	address, err := getAddressFromSig(signature, hash)
	if err != nil {
		return nil, err
	}
	return &SignedBid{
		AmountWei: amountWei,
		L1Block:   l1Block,
		Address:   address,
		Signature: signature,
	}, nil
}

func MustCreateSignedBid(amountWei *big.Int, l1Block *big.Int, privateKey *ecdsa.PrivateKey) *SignedBid {
	bid, err := CreateSignedBid(amountWei, l1Block, privateKey)
	if err != nil {
		log.Fatalf("Error creating signed bid: %v", err)
	}
	return bid
}

func (b *SignedBid) Verify() bool {
	hash := getDataHash(b.AmountWei, b.L1Block)
	sigPublicKey, err := crypto.SigToPub(hash.Bytes(), b.Signature)
	if err != nil {
		return false
	}
	signerAddress := crypto.PubkeyToAddress(*sigPublicKey)
	return signerAddress == b.Address
}

func EncodeSignedBid(bid *SignedBid) string {
	jsonData, err := json.Marshal(bid)
	if err != nil {
		log.Fatalf("Error encoding SignedBid to JSON: %v", err)
	}
	return string(jsonData)
}

func DecodeSignedBid(jsonData string) (*SignedBid, error) {
	var bid SignedBid
	err := json.Unmarshal([]byte(jsonData), &bid)
	if err != nil {
		log.Printf("Error decoding JSON to SignedBid: %v", err)
		return nil, err
	}
	return &bid, nil
}

func getDataHash(amountWei *big.Int, l1Block *big.Int) common.Hash {
	data := fmt.Sprintf("%s%s", amountWei.String(), l1Block.String())
	return crypto.Keccak256Hash([]byte(data))
}

func getAddressFromSig(signature hexutil.Bytes, hash common.Hash) (common.Address, error) {
	sigPublicKey, err := crypto.SigToPub(hash.Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*sigPublicKey), nil
}
