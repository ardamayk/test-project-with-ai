import { describe, expect, it, vi } from 'vitest'
import { confirmDelete } from './use-delete-library'

describe('confirmDelete', () => {
  it('delegates to window.confirm', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    expect(confirmDelete('Delete album?')).toBe(true)
    expect(confirm).toHaveBeenCalledWith('Delete album?')
    confirm.mockRestore()
  })
})
