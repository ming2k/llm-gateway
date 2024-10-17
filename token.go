package main

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
)

func GetAccessToken(clientEmail, privateKeyPEM, privateKeyID string) (string, error) {
	// 解析私钥
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return "", fmt.Errorf("parsing private key: %w", err)
	}

	// 生成 JWT
	jwtToken, err := generateJWT(clientEmail, privateKey, privateKeyID)
	if err != nil {
		return "", fmt.Errorf("generating JWT: %w", err)
	}

	// 交换 JWT 获取访问令牌
	accessToken, err := exchangeJWTForAccessToken(jwtToken)
	if err != nil {
		return "", fmt.Errorf("exchanging JWT for access token: %w", err)
	}

	return accessToken, nil
}

func generateJWT(clientEmail string, privateKey *rsa.PrivateKey, privateKeyID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   clientEmail,
		"aud":   "https://www.googleapis.com/oauth2/v4/token",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"scope": "https://www.googleapis.com/auth/cloud-platform",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = privateKeyID

	return token.SignedString(privateKey)
}

func exchangeJWTForAccessToken(jwtToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", jwtToken)

	resp, err := http.Post("https://www.googleapis.com/oauth2/v4/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed: status=%d, body=%s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return tokenResp.AccessToken, nil
}
