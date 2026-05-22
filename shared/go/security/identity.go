// Package security provides SM2/SM3-based cryptographic operations for Go Home.
//
// 本包实现了 Go Home 系统的核心密码学原语，包括：
//   - SM2 密钥对管理，用于设备身份认证和 P2P 密钥交换
//   - HMAC-SM3 时间窗口密钥生成与验证，用于防重放攻击
//   - SM3-HMAC 密码哈希与验证
//   - 密码学安全随机令牌生成
//
// 全链路采用国密体系（SM2/SM3/SM4），跨端（Go/Android）协议输出必须兼容。
package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tjfoc/gmsm/sm2"
	"github.com/tjfoc/gmsm/sm3"
	"github.com/tjfoc/gmsm/x509"
)

// Identity 持有 SM2 密钥对，用于设备身份认证和 P2P 加密密钥交换。
//
// 每个设备在首次运行时生成 Identity 并持久化私钥到磁盘，后续运行时加载。
// 公钥以 PEM 编码存储，便于在网络中传输。
// 私钥以 PEM 明文存储（无密码保护），文件权限为 0600。
type Identity struct {
	// Private 是 SM2 私钥，用于解密和签名操作。
	Private *sm2.PrivateKey
	// PublicPEM 是 PEM 编码的 SM2 公钥字符串，用于在控制包中传输（如 Hello 握手）和派生 DeviceID。
	PublicPEM string
}

// LoadOrCreateIdentity 从指定路径加载已有的 SM2 身份，如果文件不存在则生成新的。
//
// 首次运行时：生成 SM2 密钥对 → PEM 编码写入文件（权限 0600）→ 返回 Identity。
// 后续运行时：读取文件 → 解析 PEM → 返回 Identity。
// 父目录不存在时自动创建（权限 0700）。
func LoadOrCreateIdentity(path string) (*Identity, error) {
	if path == "" {
		return nil, errors.New("identity path is required")
	}
	if pem, err := os.ReadFile(path); err == nil {
		return ParseIdentity(pem)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	privatePEM, err := x509.WritePrivateKeyToPem(key, nil)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, privatePEM, 0o600); err != nil {
		return nil, err
	}
	return identityFromPrivate(key)
}

// ParseIdentity 从 PEM 编码的字节中解析 SM2 私钥并返回对应的 Identity。
// PEM 数据必须包含无密码保护的 SM2 私钥。
func ParseIdentity(privatePEM []byte) (*Identity, error) {
	key, err := x509.ReadPrivateKeyFromPem(privatePEM, nil)
	if err != nil {
		return nil, err
	}
	return identityFromPrivate(key)
}

// EncryptForPublicKey 使用 SM2 非对称加密对明文进行加密。
// 这是 P2P 隧道建立时加密会话密钥的主要机制：
// 客户端用家庭服务器的公钥加密 SM4 会话密钥，家庭服务器用私钥解密。
func EncryptForPublicKey(publicPEM string, plaintext []byte) ([]byte, error) {
	publicKey, err := x509.ReadPublicKeyFromPem([]byte(publicPEM))
	if err != nil {
		return nil, err
	}
	return sm2.EncryptAsn1(publicKey, plaintext, rand.Reader)
}

// Decrypt 使用 SM2 私钥对密文进行解密。
// 如果 Identity 为 nil 或私钥为 nil，返回错误。
func (i *Identity) Decrypt(ciphertext []byte) ([]byte, error) {
	if i == nil || i.Private == nil {
		return nil, errors.New("identity private key is not available")
	}
	return sm2.DecryptAsn1(i.Private, ciphertext)
}

// DeviceID 从公钥派生设备唯一标识。
// 算法：SM3(PublicPEM) 取前 16 字节 → 32 个 hex 字符 → 添加前缀。
// 例如：prefix="home" → "home-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
func (i *Identity) DeviceID(prefix string) string {
	digest := sm3.New()
	// sm3.digest.Write 永远不会返回错误，忽略返回值是安全的
	_, _ = digest.Write([]byte(i.PublicPEM))
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(digest.Sum(nil)[:16]))
}

// identityFromPrivate 从 SM2 私钥创建 Identity，自动推导公钥的 PEM 编码。
func identityFromPrivate(key *sm2.PrivateKey) (*Identity, error) {
	publicPEM, err := x509.WritePublicKeyToPem(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return &Identity{Private: key, PublicPEM: string(publicPEM)}, nil
}
