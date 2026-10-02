import { createContext, useContext } from 'react'

export const AdminApiSlot = createContext(null)

export function useAdminApiSlot() {
  return useContext(AdminApiSlot)
}
