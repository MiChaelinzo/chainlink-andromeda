package keystore

import (
	"context"
	"fmt"
	"math/big"

	"github.com/pkg/errors"
	"github.com/smartcontractkit/caigo"

	"github.com/smartcontractkit/chainlink-relay/pkg/loop"
	adapters "github.com/smartcontractkit/chainlink-relay/pkg/loop/adapters/starknet"
	"github.com/smartcontractkit/chainlink/v2/core/services/keystore/keys/starkkey"
)

//go:generate mockery --name StarkNet --output ./mocks/ --case=underscore --filename starknet.go
type StarkNet interface {
	Get(id string) (starkkey.Key, error)
	GetAll() ([]starkkey.Key, error)
	Create() (starkkey.Key, error)
	Add(key starkkey.Key) error
	Delete(id string) (starkkey.Key, error)
	Import(keyJSON []byte, password string) (starkkey.Key, error)
	Export(id string, password string) ([]byte, error)
	EnsureKey() error
}

type starknet struct {
	*keyManager
}

var _ StarkNet = &starknet{}

func newStarkNetKeyStore(km *keyManager) *starknet {
	return &starknet{
		km,
	}
}

func (ks *starknet) Get(id string) (starkkey.Key, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()
	if ks.isLocked() {
		return starkkey.Key{}, ErrLocked
	}
	return ks.getByID(id)
}

func (ks *starknet) GetAll() (keys []starkkey.Key, _ error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()
	if ks.isLocked() {
		return nil, ErrLocked
	}
	for _, key := range ks.keyRing.StarkNet {
		keys = append(keys, key)
	}
	return keys, nil
}

func (ks *starknet) Create() (starkkey.Key, error) {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return starkkey.Key{}, ErrLocked
	}
	key, err := starkkey.New()
	if err != nil {
		return starkkey.Key{}, err
	}
	return key, ks.safeAddKey(key)
}

func (ks *starknet) Add(key starkkey.Key) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return ErrLocked
	}
	if _, found := ks.keyRing.StarkNet[key.ID()]; found {
		return fmt.Errorf("key with ID %s already exists", key.ID())
	}
	return ks.safeAddKey(key)
}

func (ks *starknet) Delete(id string) (starkkey.Key, error) {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return starkkey.Key{}, ErrLocked
	}
	key, err := ks.getByID(id)
	if err != nil {
		return starkkey.Key{}, err
	}
	err = ks.safeRemoveKey(key)
	return key, err
}

func (ks *starknet) Import(keyJSON []byte, password string) (starkkey.Key, error) {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return starkkey.Key{}, ErrLocked
	}
	key, err := starkkey.FromEncryptedJSON(keyJSON, password)
	if err != nil {
		return starkkey.Key{}, errors.Wrap(err, "StarkNetKeyStore#ImportKey failed to decrypt key")
	}
	if _, found := ks.keyRing.StarkNet[key.ID()]; found {
		return starkkey.Key{}, fmt.Errorf("key with ID %s already exists", key.ID())
	}
	return key, ks.keyManager.safeAddKey(key)
}

func (ks *starknet) Export(id string, password string) ([]byte, error) {
	ks.lock.RLock()
	defer ks.lock.RUnlock()
	if ks.isLocked() {
		return nil, ErrLocked
	}
	key, err := ks.getByID(id)
	if err != nil {
		return nil, err
	}
	return starkkey.ToEncryptedJSON(key, password, ks.scryptParams)
}

func (ks *starknet) EnsureKey() error {
	ks.lock.Lock()
	defer ks.lock.Unlock()
	if ks.isLocked() {
		return ErrLocked
	}
	if len(ks.keyRing.StarkNet) > 0 {
		return nil
	}

	key, err := starkkey.New()
	if err != nil {
		return err
	}

	ks.logger.Infof("Created StarkNet key with ID %s", key.ID())

	return ks.safeAddKey(key)
}

func (ks *starknet) getByID(id string) (starkkey.Key, error) {
	key, found := ks.keyRing.StarkNet[id]
	if !found {
		return starkkey.Key{}, KeyNotFoundError{ID: id, KeyType: "StarkNet"}
	}
	return key, nil
}

// StarkNetKeystoreAdapter is a starknet-specific adaption layer to translate between the generic Loop Keystore (bytes) and
// the type specific caigo Keystore (big.Int)
// The loop.Keystore must be produce a byte representation that can be parsed by the Decode func implementation
// Users of the interface are responsible to ensure this compatibility.
type StarkNetKeystoreAdapter interface {
	caigo.Keystore
	// Loopp must return a LOOPp Keystore implementation whose Sign func
	// is compatible with the [Decode] func implementation
	Loopp() loop.Keystore
	// Decode translates from the raw signature of the LOOPp Keystore to that of the Caigo Keystore
	Decode(ctx context.Context, rawSignature []byte) (*big.Int, *big.Int, error)
}

// keystoreAdapter implements [KeystoreAdapter]
type keystoreAdapter struct {
	looppKs loop.Keystore
}

// NewStarkNetKeystoreAdapter instantiates the KeystoreAdapter interface
// Callers are responsible for ensuring that the given LOOPp Keystore encodes
// signatures that can be parsed by the Decode function
func NewStarkNetKeystoreAdapter(lk loop.Keystore) StarkNetKeystoreAdapter {
	return &keystoreAdapter{looppKs: lk}
}

// Sign implements the caigo Keystore Sign func.
func (ca *keystoreAdapter) Sign(ctx context.Context, senderAddress string, hash *big.Int) (*big.Int, *big.Int, error) {
	raw, err := ca.looppKs.Sign(ctx, senderAddress, hash.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("error computing loopp keystore signature: %w", err)
	}
	return ca.Decode(ctx, raw)
}

func (ca *keystoreAdapter) Decode(ctx context.Context, rawSignature []byte) (x *big.Int, y *big.Int, err error) {
	starknetSig, serr := adapters.SignatureFromBytes(rawSignature)
	if serr != nil {
		return nil, nil, fmt.Errorf("error creating starknet signature from raw signature: %w", serr)
	}
	return starknetSig.Ints()
}

func (ca *keystoreAdapter) Loopp() loop.Keystore {
	return ca.looppKs
}

// StarknetLooppSigner implements [loop.Keystore] interface and the requirements
// of signature d/encoding of the [KeystoreAdapter]
type StarknetLooppSigner struct {
	StarkNet
}

func NewStarkNetLooppKeystore(ks StarkNet) *StarknetLooppSigner {
	return &StarknetLooppSigner{StarkNet: ks}
}

var _ loop.Keystore = &StarknetLooppSigner{}

// Sign implements [loop.Keystore]
// hash is expected to be the byte representation of big.Int
// the return []byte is encodes a starknet signature per [signature.bytes]
func (lk *StarknetLooppSigner) Sign(ctx context.Context, id string, hash []byte) ([]byte, error) {

	k, err := lk.Get(id)
	if err != nil {
		return nil, err
	}
	// loopp spec requires passing nil hash to check existence of id
	if hash == nil {
		return nil, nil
	}

	starkHash := new(big.Int).SetBytes(hash)
	x, y, err := caigo.Curve.Sign(starkHash, k.ToPrivKey())
	if err != nil {
		return nil, fmt.Errorf("error signing data with curve: %w", err)
	}

	sig, err := adapters.SignatureFromBigInts(x, y)
	if err != nil {
		return nil, err
	}
	return sig.Bytes()
}

// TODO what is this supposed to return for starknet?
func (lk *StarknetLooppSigner) Accounts(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("unimplemented")
}
