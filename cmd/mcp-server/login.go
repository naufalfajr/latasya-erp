package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// deviceCode is 32 random bytes, base64url-encoded (43 chars). Only its
// SHA-256 hash is ever stored server-side.
func deviceCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// userCodeAlphabet is Crockford-style base32 without ambiguous glyphs.
const userCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func userCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i, b := range buf {
		out[i] = userCodeAlphabet[int(b)%len(userCodeAlphabet)]
	}
	return string(out), nil
}

type devicePollResult struct {
	Status    string   `json:"status"`
	Token     string   `json:"token"`
	TokenID   int      `json:"token_id"`
	Scopes    []string `json:"scopes"`
	Username  string   `json:"username"`
	ExpiresAt *string  `json:"expires_at"`
}

func runLogin(baseURL string) error {
	if err := requireHTTPS(baseURL); err != nil {
		return err
	}
	dev, err := deviceCode()
	if err != nil {
		return err
	}
	code, err := userCode()
	if err != nil {
		return err
	}
	client := fmt.Sprintf("latasya-mcp (%s/%s)", runtime.GOOS, runtime.GOARCH)
	authorizeURL := baseURL + "/dashboard/mcp/authorize?code=" + code

	fmt.Printf("Opening your browser:\n  %s\n\nApprove only if this code matches: %s\n", authorizeURL, code)
	openBrowser(authorizeURL)

	hc := &http.Client{Timeout: 15 * time.Second}
	body := func() []byte {
		b, _ := json.Marshal(map[string]string{"device_code": dev, "user_code": code, "client": client})
		return b
	}()

	deadline := time.Now().Add(10 * time.Minute)
	for {
		res, done, err := pollDevice(hc, baseURL, body)
		if err != nil {
			return err
		}
		if done {
			cfg := &config{
				BaseURL:  baseURL,
				Token:    res.Token,
				TokenID:  res.TokenID,
				Username: res.Username,
				Scopes:   res.Scopes,
			}
			if res.ExpiresAt != nil {
				cfg.ExpiresAt = *res.ExpiresAt
			}
			if err := cfg.save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("Connected as %s (%d capabilities).\n", res.Username, len(res.Scopes))
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("login timed out after 10 minutes — re-run `latasya-mcp login`")
		}
		time.Sleep(5 * time.Second)
	}
}

// pollDevice posts one poll and interprets the status. done=true means the
// token was minted into res.
func pollDevice(hc *http.Client, baseURL string, body []byte) (devicePollResult, bool, error) {
	var res devicePollResult
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/mcp/device", bytes.NewReader(body))
	if err != nil {
		return res, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return res, false, err
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch resp.StatusCode {
	case http.StatusAccepted:
		fmt.Print(".") // pending heartbeat on one line
		return res, false, nil
	case http.StatusOK:
		var env struct {
			Data devicePollResult `json:"data"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			return res, false, fmt.Errorf("malformed approval response: %w", err)
		}
		fmt.Println()
		return env.Data, true, nil
	case http.StatusForbidden:
		fmt.Println()
		return res, false, fmt.Errorf("login denied in the browser")
	case http.StatusGone:
		fmt.Println()
		return res, false, fmt.Errorf("login expired or restarted — re-run `latasya-mcp login`")
	case http.StatusTooManyRequests:
		return res, false, fmt.Errorf("rate limited; wait a few minutes and re-run `latasya-mcp login`")
	default:
		return res, false, fmt.Errorf("device login failed: %d %s", resp.StatusCode, data)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open a browser automatically: %v", err)
	}
}

// runStatus prints the persisted credential summary.
func runStatus() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Printf("Connected as %s at %s (%d capabilities)", cfg.Username, cfg.BaseURL, len(cfg.Scopes))
	if cfg.ExpiresAt == "" {
		fmt.Println("; token never expires.")
	} else {
		exp, err := time.Parse(time.RFC3339, cfg.ExpiresAt)
		if err != nil {
			fmt.Printf("; token expires %s.\n", cfg.ExpiresAt)
		} else {
			fmt.Printf("; token expires in %.0f days.\n", time.Until(exp).Hours()/24)
		}
	}
	return nil
}

// runLogout self-revokes the token and wipes the config.
func runLogout() error {
	if err := requireHTTPS(envBaseURL()); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		// Nothing to revoke; still clear any stale file.
		_ = os.Remove(configPath())
		fmt.Println("Not logged in.")
		return nil
	}
	client := newAPIClient(cfg.BaseURL, cfg.Token)
	_, _, err = client.call(http.MethodDelete, "/api/v1/auth/token", nil, "", "")
	if err != nil {
		fmt.Printf("Warning: could not revoke the token server-side (%v); revoke it in the ERP under API Tokens.\n", redact(err.Error(), cfg.Token))
	}
	if os.Getenv("LATASYA_MCP_TOKEN") != "" {
		// The env token is now dead, but the config file's own token was
		// never in play — leave it and say so.
		fmt.Println("Disconnected; the LATASYA_MCP_TOKEN was revoked (config file untouched).")
		return nil
	}
	if err := os.Remove(configPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Disconnected; token revoked.")
	return nil
}
