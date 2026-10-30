// mock-tapd 提供一个最小化的 TAPD OpenAPI 模拟服务，便于在没有真实
// 凭据时本地联调连接设置、工作区同步与用例生成链路。
//
//	go run ./cmd/mock-tapd        # 监听 :9099
//
// 账号：qa-user / qa-pass（Basic 认证），工作区 8888。
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	server := &http.Server{
		Addr: ":9099",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, password, ok := r.BasicAuth()
			if !ok || user != "qa-user" || password != "qa-pass" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "401", "message": "authorization failed"})
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			switch r.URL.Path {
			case "/workspaces/projects":
				writeData(w, map[string]any{"Workspace": map[string]any{"id": "8888", "name": "冒烟测试空间"}})
			case "/bugs":
				writeData(w,
					map[string]any{"Bug": map[string]any{
						"id": "10001", "title": "下单页金额显示为 0",
						"description": "<p>1.&nbsp;选择商品下单</p><p>2. 打开订单详情，金额列显示为 0</p>",
						"status":      "new", "priority": "1",
					}},
					map[string]any{"Bug": map[string]any{
						"id": "10002", "title": "退款后优惠券未返还",
						"description": "退款成功后，订单使用的优惠券没有回到账户",
						"status":      "resolved", "priority": "2",
					}},
				)
			case "/stories":
				writeData(w, map[string]any{"Story": map[string]any{
					"id": "20001", "name": "支持分期付款",
					"description": "结算页支持 3/6/12 期分期", "status": "develop", "priority": "2",
				}})
			default:
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
			}
		}),
	}
	log.Println("mock TAPD listening on :9099")
	log.Fatal(server.ListenAndServe())
}

func writeData(w http.ResponseWriter, items ...map[string]any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
}
