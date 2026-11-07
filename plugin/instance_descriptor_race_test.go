package plugin

// instance_descriptor_race_test.go — Instance 读方法与描述符 COW 替换的竞争回归测试
//
// 回归背景：finalizeRegistration 在合并未声明依赖时会以 COW 方式替换
// instance.desc（持 inst.mu），而 Name()/dependencies() 直接裸读 pi.desc。
// *Instance 通过 Manager.Get 对外暴露，外部并发调用即构成数据竞争（-race 可稳定
// 复现）。读路径统一改走加锁的 descriptor()。

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstance_ReadMethodsRaceWithDescriptorSwap(t *testing.T) {
	pm := NewManager(nil)

	var inst *Instance
	require.NoError(t, pm.Register(&Descriptor{
		Name: "desc-race",
		Setup: func(ctx *SetupContext) (any, error) {
			inst = ctx.instance
			return nil, nil
		},
	}))
	require.NotNil(t, inst)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = inst.Name()
				_ = inst.dependencies()
				_ = inst.Metadata()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			d := *inst.descriptor()
			d.Deps = []string{"dep-a", "dep-b"}
			inst.setDescriptor(&d)
		}
		close(stop)
	}()
	wg.Wait()
}
