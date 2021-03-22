package orm

//	"fmt"
//"testing"
//"time"

/*
// 测试多线程
func TestAsync(t *testing.T) {
	// 注册Model
	Registry.TestAddModel("base",
		/*	new(module.SysModelData),
			new(module.SysValues),
			new(module.SysAttachment),
			new(module.SysModelAccess),
			new(SysModule),
			new(SysModuleCategory),
			new(SysModuleDependency),
			new(SysModel),
			new(SysModelData),
			new(SysModelField),
			new(SysMenu),
			new(SysAction),
			new(SysView),
			new(SysEvent),*/
/*		new(SysLog),
	)

	for i := 0; i < 1000; i++ {
		//fmt.Println("pro:", i)
		go func(i int) {
			//fmt.Println("Clear", i)

			//  获取Session
			lSess := RegistrySession.GetSession()
			// 修改登录信息
			lSess.AuthInfo("id", "1")
			lSess.AuthInfo("passport", "admin")
			lSess.AuthInfo("password", "admin")

			//fmt.Println("lSess", lSess)

			model := lSess.GetModel("sys.log")
			//fmt.Println("model", model != nil)
			if model != nil {
				fmt.Println("Clear", i)
				//lValues["company_id"] = nil
				//lValues["partner_id"] = nil
				//model.Create(lValues)

				// 更新
				//model.Write([]string{"3"}, lValues)

				// 删除
				//model.Unlink("7")

				//model.Read([]string{"1"}, nil)
			} else {
				fmt.Println("Out", i)
			}

		}(i)
	}

	time.Sleep(5 * time.Second)
	//<-make(chan int)
}
*/
