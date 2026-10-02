import { createContext, useContext } from 'react'

const ApiContext = createContext(null)

export function ApiProvider({ client, children }) {
  return <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
}

export function useApi() {
  const client = useContext(ApiContext)
  if (!client) throw new Error('useApi requires ApiProvider')
  return client
}
