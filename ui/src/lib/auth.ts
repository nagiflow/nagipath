// /logout is still internal/web's old form-POST handler (auth.go), not
// internal/api — it isn't ported until the dedicated auth-pages phase. Same
// CSRF check either way (internal/api/middleware.go's checkCSRF is a copy of
// internal/web's), so a header-only POST from here works against it today.
export async function logout(csrfToken: string) {
  await fetch('/logout', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    credentials: 'same-origin',
  })
  window.location.assign('/login')
}
