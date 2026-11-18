package trader

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------- approval token ----------
//
// The token is what makes the gate an enforcement point instead of advice: the
// order path refuses to execute without one, and the token is an HMAC over the
// exact intent, so neither the model nor a caller can widen an approval after
// the fact (different size, symbol, stop or policy version = different MAC).
//
// The key is per-process and never leaves the process. A restart invalidates
// outstanding tokens — the safe direction: the run must re-ask the gate.
var riskTokenKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic("trader: cannot initialize risk token key: " + err.Error())
	}
	return k
}()

// tokenTTL bounds how long an approval stays usable. A long-lived token is an
// approval that outlives the market state it was granted against.
const tokenTTL = 5 * time.Minute

// intentMAC binds policy version, symbol, side, quantity, price, stop and the
// client order id, plus an expiry, into one value.
func intentMAC(policyVersion string, in OrderIntent, expiresUnix int64) string {
	mac := hmac.New(sha256.New, riskTokenKey)
	parts := []string{
		policyVersion, in.Symbol, in.Side,
		strconv.FormatFloat(in.Quantity, 'f', -1, 64),
		strconv.FormatFloat(in.Price, 'f', -1, 64),
		strconv.FormatFloat(in.StopLoss, 'f', -1, 64),
		in.ClientOrderID,
		strconv.FormatInt(expiresUnix, 10),
	}
	mac.Write([]byte(strings.Join(parts, fieldSep)))
	return hex.EncodeToString(mac.Sum(nil))
}

// issueToken returns "<expiry>.<mac>".
func issueToken(policyVersion string, in OrderIntent, now time.Time) string {
	exp := now.Add(tokenTTL).Unix()
	return strconv.FormatInt(exp, 10) + "." + intentMAC(policyVersion, in, exp)
}

// VerifyToken checks that a token authorizes exactly this intent under this
// policy version and has not expired. It is the order path's only key.
func VerifyToken(token, policyVersion string, in OrderIntent, now time.Time) error {
	if token == "" {
		return fmt.Errorf("risk token: missing (the order was not risk-approved)")
	}
	dot := strings.IndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 {
		return fmt.Errorf("risk token: malformed")
	}
	exp, err := strconv.ParseInt(token[:dot], 10, 64)
	if err != nil {
		return fmt.Errorf("risk token: malformed expiry")
	}
	if now.Unix() > exp {
		return fmt.Errorf("risk token: expired")
	}
	want := intentMAC(policyVersion, in, exp)
	if !hmac.Equal([]byte(want), []byte(token[dot+1:])) {
		return fmt.Errorf("risk token: does not authorize this intent")
	}
	return nil
}
