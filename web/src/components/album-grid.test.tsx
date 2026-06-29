import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AlbumGrid } from './album-grid'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    ...props
  }: {
    children: React.ReactNode
    to: string
    params?: Record<string, string>
  }) => <a href={props.to}>{children}</a>,
  useNavigate: () => vi.fn(),
}))

vi.mock('#/hooks/use-delete-library', () => ({
  useDeleteAlbum: () => ({ mutate: vi.fn(), isPending: false }),
  confirmDelete: () => false,
}))

describe('AlbumGrid', () => {
  it('renders empty state', () => {
    render(<AlbumGrid albums={[]} />)
    expect(screen.getByText(/No albums yet/)).toBeTruthy()
  })
})
