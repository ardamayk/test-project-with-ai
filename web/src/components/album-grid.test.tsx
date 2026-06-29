import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AlbumGrid } from './album-grid'

describe('AlbumGrid', () => {
  it('renders empty state', () => {
    render(<AlbumGrid albums={[]} />)
    expect(screen.getByText(/No albums yet/)).toBeTruthy()
  })
})
