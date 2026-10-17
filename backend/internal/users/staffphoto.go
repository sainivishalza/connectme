package users

import "context"

// ReadPhotoUnchecked returns a stored photo without applying viewer
// visibility rules. It exists for the moderation service, which performs its
// own authorisation (open report + audit entry); never expose it directly.
func (s *Service) ReadPhotoUnchecked(ctx context.Context, photoID string) ([]byte, error) {
	return s.media.Get(ctx, photoID)
}
