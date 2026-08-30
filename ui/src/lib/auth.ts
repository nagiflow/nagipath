import { api } from '../api/client'

export async function logout() {
  await api.postAction('/logout')
  window.location.assign('/login')
}
