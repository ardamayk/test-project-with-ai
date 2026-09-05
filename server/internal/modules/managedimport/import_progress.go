package managedimport

func (service *Service) setUploadPhase(jobID, phase string) {
	service.activeUploadsMu.Lock()
	defer service.activeUploadsMu.Unlock()
	if active := service.activeUploads[jobID]; active != nil {
		active.phase = phase
	}
}

func (service *Service) addBatchProgress(batch *Batch) {
	service.activeUploadsMu.Lock()
	defer service.activeUploadsMu.Unlock()
	for index := range batch.Files {
		file := &batch.Files[index]
		file.Phase = batchFilePhase(*file, batch.Status)
		if active := service.activeUploads[file.JobID]; active != nil && file.State == BATCH_FILE_UNRESOLVED {
			file.Phase = active.phase
			file.TransferredBytes = active.transferredBytes
			file.TotalBytes = active.totalBytes
		}
	}
}

func batchFilePhase(file BatchFile, status BatchStatus) string {
	if file.State == BATCH_FILE_COMPLETED || file.Outcome != "" {
		return "completed"
	}
	if status == BATCH_STATUS_CONFIRMING && file.Selected {
		return "committing"
	}
	if file.State == BATCH_FILE_ACCEPTED {
		return "ready"
	}
	if file.State == BATCH_FILE_REJECTED || file.ErrorCode != "" {
		return "failed"
	}
	return "queued"
}
