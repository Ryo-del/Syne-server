package files

import "fmt"

const (
	gib = int64(1) << 30

	// Unlimited — значение лимита «без ограничений» (преподаватели).
	Unlimited int64 = -1
)

// QuotaPerUser = AllMemory / CountUsers, в байтах. Гигабайты считаются как
// GiB (2^30), поэтому 500 / 50 дают ровно 10 GiB на пользователя.
func QuotaPerUser(allMemoryGB, countUsers int) (int64, error) {
	if allMemoryGB < 1 || countUsers < 1 {
		return 0, fmt.Errorf("files: invalid quota settings (all_memory_gb=%d, count_users=%d)",
			allMemoryGB, countUsers)
	}
	return int64(allMemoryGB) * gib / int64(countUsers), nil
}

// LimitForRole — лимит папки по роли её ВЛАДЕЛЬЦА (а не того, кто пишет):
// у преподавателей лимита нет.
func LimitForRole(ownerRole string, perUser int64) int64 {
	if IsTeacherRole(ownerRole) {
		return Unlimited
	}
	return perUser
}
