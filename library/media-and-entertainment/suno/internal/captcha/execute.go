// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// The invisible hCaptcha solve: render an offscreen invisible widget bound to
// Suno's sitekey + first-party hosts, then await hcaptcha.execute(). Mirrors
// paperfoot/suno-cli's proven render_and_execute payload.

package captcha

import (
	"fmt"
	"strings"
)

// solveJS returns the page script that renders an invisible hCaptcha widget and
// resolves to the token string (or "ERR:<reason>").
func solveJS() string {
	return fmt.Sprintf(`
(async () => {
  try {
    const div = document.createElement('div');
    div.style.cssText = 'position:fixed;top:-9999px;left:-9999px;';
    document.body.appendChild(div);
    const id = hcaptcha.render(div, {
      sitekey: '%s',
      size:'invisible',
      sentry: false,
      endpoint: '%s',
      assethost: '%s',
      imghost: '%s',
      reportapi: '%s',
    });
    const r = await hcaptcha.execute(id, { async: true });
    return (r && r.response) ? r.response : '';
  } catch (e) {
    return 'ERR:' + String(e);
  }
})()`, SunoHCaptchaSitekey, hcaptchaEndpoint, hcaptchaAssetHost, hcaptchaImgHost, hcaptchaReportAPI)
}

// classifyToken interprets the raw JS result:
//   - non-empty, non-ERR        -> (token, false, nil)
//   - empty                     -> ("", true, nil)   interactive needed
//   - ERR:...challenge-expired  -> ("", true, nil)   interactive needed
//   - any other ERR:...         -> ("", false, error) hard infra/JS failure
func classifyToken(raw string) (token string, interactiveNeeded bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true, nil
	}
	if strings.HasPrefix(raw, "ERR:") {
		reason := strings.TrimPrefix(raw, "ERR:")
		if strings.Contains(strings.ToLower(reason), "challenge-expired") ||
			strings.Contains(strings.ToLower(reason), "challenge expired") {
			return "", true, nil
		}
		return "", false, fmt.Errorf("hcaptcha solver: %s", reason)
	}
	return raw, false, nil
}
