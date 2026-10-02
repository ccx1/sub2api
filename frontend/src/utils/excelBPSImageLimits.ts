// Administrative ceilings; keep in sync with backend/internal/service/basispoints/image_capacity.go.
// Raising a ceiling does not change saved settings or allocate resources.
export const excelBPSImageLimits = {
  bodyMiB: 1024,
  minBudgetMiB: 512,
  budgetMiB: 65536,
  requests: 4096,
  imageMiB: 512,
  totalMiB: 512,
  images: 65536,
  storageMiB: 262144,
  storageEntries: 1048576,
  ttlMinutes: 10080,
} as const;
