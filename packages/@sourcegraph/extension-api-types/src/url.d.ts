export type ReadonlySearchParams = Readonly<Omit<URLSearchParams, 'append' | 'set' | 'delete'>>
export type ReadonlyURL = Readonly<Omit<URL, 'searchParams'> & { searchParams: ReadonlySearchParams }>
