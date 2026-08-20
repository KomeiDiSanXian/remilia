package main

import (
	"fmt"
	"log"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
)

// SimpleLogger 简单的日志实现
type SimpleLogger struct{}

func (l *SimpleLogger) Debugf(format string, args ...any) {
	log.Printf("[DEBUG] "+format, args...)
}

func (l *SimpleLogger) Infof(format string, args ...any) {
	log.Printf("[INFO] "+format, args...)
}

func (l *SimpleLogger) Warnf(format string, args ...any) {
	log.Printf("[WARN] "+format, args...)
}

func (l *SimpleLogger) Errorf(format string, args ...any) {
	log.Printf("[ERROR] "+format, args...)
}

func main() {
	fmt.Println("=== HTTP Client 演示 ===")

	// 1. 基本 GET 请求
	fmt.Println("1️⃣  基本 GET 请求")
	fmt.Println("----------------------------------------")

	resp, err := httpclient.Get("https://httpbin.org/get").
		SetQuery("name", "alice").
		SetQuery("age", "25").
		Do()

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Status: %d %s\n", resp.StatusCode, resp.Status)

		if body, err := resp.String(); err == nil {
			fmt.Printf("Response: %s\n", body[:100]+"...")
		}
	}

	fmt.Println()

	// 2. POST JSON 请求
	fmt.Println("2️⃣  POST JSON 请求")
	fmt.Println("----------------------------------------")

	userData := map[string]any{
		"name":  "Bob",
		"age":   30,
		"email": "bob@example.com",
	}

	resp, err = httpclient.Post("https://httpbin.org/post").
		SetJSON(userData).
		Do()

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)

		if jsonResult, err := resp.JSON(); err == nil {
			fmt.Printf("Received data: %s\n", jsonResult.Get("json").String())
		}
	}

	fmt.Println()

	// 3. 使用客户端配置
	fmt.Println("3️⃣  使用客户端配置")
	fmt.Println("----------------------------------------")

	client := httpclient.NewClient().
		SetBaseURL("https://httpbin.org").
		SetTimeout(10*time.Second).
		SetHeader("User-Agent", "HTTPClient-Demo/1.0").
		Use(httpclient.UserAgentMiddleware("CustomAgent/2.0"))

	resp, err = client.Get("/headers").Do()
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)

		if jsonResult, err := resp.JSON(); err == nil {
			fmt.Printf("User-Agent: %s\n",
				jsonResult.Get("headers.User-Agent").String())
		}
	}

	fmt.Println()

	// 4. 表单提交
	fmt.Println("4️⃣  表单提交")
	fmt.Println("----------------------------------------")

	resp, err = httpclient.Post("https://httpbin.org/post").
		SetForm(map[string]string{
			"username": "alice",
			"password": "secret",
		}).
		Do()

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)

		if jsonResult, err := resp.JSON(); err == nil {
			fmt.Printf("Form data: %s\n", jsonResult.Get("form").String())
		}
	}

	fmt.Println()

	// 5. 使用认证中间件
	fmt.Println("5️⃣  使用认证中间件")
	fmt.Println("----------------------------------------")

	authClient := httpclient.NewClient().
		Use(httpclient.AuthBearerMiddleware("my-secret-token"))

	resp, err = authClient.Get("https://httpbin.org/bearer").Do()
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)

		if resp.IsSuccess() {
			fmt.Println("✓ Authentication successful")
		}
	}

	fmt.Println()

	// 6. 链式调用
	fmt.Println("6️⃣  链式调用")
	fmt.Println("----------------------------------------")

	result, err := httpclient.Get("https://httpbin.org/json").
		SetHeader("Accept", "application/json").
		SetTimeout(5 * time.Second).
		DoJSON()

	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Slideshow title: %s\n",
			result.Get("slideshow.title").String())
	}

	fmt.Println()

	// 7. 错误处理
	fmt.Println("7️⃣  错误处理")
	fmt.Println("----------------------------------------")

	resp, err = httpclient.Get("https://httpbin.org/status/404").Do()
	if err != nil {
		log.Printf("Request error: %v\n", err)
	} else {
		defer resp.Close()

		if resp.IsError() {
			fmt.Printf("✗ Error response: %d\n", resp.StatusCode)
		}

		if !resp.IsSuccess() {
			fmt.Println("✗ Request was not successful")
		}
	}

	fmt.Println()

	// 8. 带重试的客户端
	fmt.Println("8️⃣  带重试的客户端")
	fmt.Println("----------------------------------------")

	retryClient := httpclient.NewClient().
		SetRetry(&httpclient.RetryConfig{
			MaxRetries:     3,
			RetryWaitTime:  1 * time.Second,
			RetryMaxWait:   3 * time.Second,
			RetryCondition: httpclient.DefaultRetryCondition,
		}).
		SetLogger(&SimpleLogger{})

	// 这会失败并重试
	resp, err = retryClient.Get("https://httpbin.org/status/500").Do()
	if err != nil {
		log.Printf("Error after retries: %v\n", err)
	} else {
		defer resp.Close()
		fmt.Printf("Final status: %d\n", resp.StatusCode)
	}

	fmt.Println()
	fmt.Println("✨ HTTP Client 演示完成！")
}
