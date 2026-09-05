package library

import "errors"

// Missing artwork is allowed; malformed artwork stays visible as a warning.
func optionalArtwork(artwork AlbumArtwork, err error) (AlbumArtwork, error) {
	if err == nil {
		return artwork, nil
	}
	var inspectionErr *InspectionError
	if !errors.As(err, &inspectionErr) {
		return artwork, err
	}
	switch inspectionErr.Code {
	case INSPECTION_ERROR_MISSING_ARTWORK:
		return AlbumArtwork{}, nil
	case INSPECTION_ERROR_INVALID_ARTWORK:
		return AlbumArtwork{Warning: "Embedded artwork is invalid; choose another cover or continue without artwork"}, nil
	default:
		return artwork, err
	}
}

// ValidateUploadedArtwork checks actual image bytes, not a filename extension.
func ValidateUploadedArtwork(data []byte) (AlbumArtwork, error) {
	format := detectArtworkFormat(data)
	if format != ARTWORK_FORMAT_JPEG && format != ARTWORK_FORMAT_PNG {
		return AlbumArtwork{}, inspectionError(INSPECTION_ERROR_INVALID_ARTWORK, "artwork", errors.New("uploaded artwork must be JPEG or PNG"))
	}
	return validateArtworkData(artworkMIMETypes[format], data)
}
