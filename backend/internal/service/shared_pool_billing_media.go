package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

var ErrSharedPoolAsyncMediaUnsupported = infraerrors.BadRequest("SHARED_POOL_ASYNC_MEDIA_UNSUPPORTED", "共享账号池暂不支持异步视频和批量图像任务，请使用普通分组")

func isSharedPoolBillingAccount(account *Account) bool {
	return account != nil && sharedPoolBillingOwnerID(account.Extra["shared_pool_owner_id"]) > 0
}

// 异步任务尚未持久化创建时的实际代理快照，不能在查询任务时按另一条代理错误分成。
func validateSharedPoolMediaBilling(account *Account, endpoint GrokMediaEndpoint) error {
	if !isSharedPoolBillingAccount(account) || endpoint == GrokMediaEndpointImagesGenerations || endpoint == GrokMediaEndpointImagesEdits {
		return nil
	}
	return ErrSharedPoolAsyncMediaUnsupported
}
