package jmux

import (
	"io"
	"net/http"
	"net/http/httptest"
	urlpkg "net/url"
	"path"
	"testing"
)

func TestRouter(t *testing.T) {
	handleAnyCode := http.StatusBadRequest

	router := NewRouter()
	router.GetFunc("/", func(c *Context) {
		c.WriteString("GET /")
	}).HandleAnyFunc(MethodsGet(), func(c *Context) {
		c.WriteError(handleAnyCode, "")
	})
	router.PostFunc("/", func(c *Context) {
		c.WriteString("POST /")
	})
	router.GetFunc("/slug1", func(c *Context) {
		c.WriteString("GET /slug1")
	})
	router.GetFunc("/slug1/", func(c *Context) {
		c.WriteString("GET /slug1/")
	})
	router.PostFunc("/slug1", func(c *Context) {
		c.WriteString("POST /slug1")
	})
	router.GetFunc("/slug1/slug1.1", func(c *Context) {
		c.WriteString("GET /slug1/slug1.1")
	})
	router.GetFunc("/slug1/slug1.1/", func(c *Context) {
		c.WriteString("GET /slug1/slug1.1/")
	})
	router.GetFunc("/slug1//slug1.1", func(c *Context) {
		c.WriteString("GET /slug1//slug1.1")
	})
	router.GetFunc("/slug2/{slug2.1}", func(c *Context) {
		c.WriteString("GET /slug2/" + c.Params["slug2.1"])
	})
	router.GetFunc("/slug2/{slug2.1}/", func(c *Context) {
		c.WriteString("GET /slug2/" + c.Params["slug2.1"] + "/")
	})
	router.GetFunc("/slug2/{slug2.1}/slug2.1.1", func(c *Context) {
		c.WriteString("GET /slug2/" + c.Params["slug2.1"] + "/slug2.1.1")
	})

	router.GetFunc("/fallback-slash", func(c *Context) {
		c.WriteString("GET /fallback-slash")
	}).MatchAny(MethodsGet())
	router.GetFunc("/fallback-slash/", func(c *Context) {
		c.WriteString("GET /fallback-slash/")
	}).MatchAny(MethodsGet())

	router.GetFunc("/fallback-slash2/", func(c *Context) {
		c.WriteString("GET /fallback-slash2/")
	}).MatchAny(MethodsGet())

	router.GetFunc("/fallback-slash3", func(c *Context) {
		c.WriteString("GET /fallback-slash3")
	}).MatchAny(MethodsGet())

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := &http.Response{}, error(nil)

	resp, err = http.Get(joinPath(ts.URL, "/slug1"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug1")

	resp, err = http.Get(joinPath(ts.URL, "/slug1/"))
	//resp, err = http.Get(ts.URL + "/slug1/")
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug1/")

	resp, err = http.Get(joinPath(ts.URL, "slug1", "slug1.1"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug1/slug1.1")

	resp, err = http.Get(joinPath(ts.URL, "slug1", "slug1.1/"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug1/slug1.1/")

	//resp, err = http.Get(joinPath(ts.URL, "slug1", "/slug1.1"))
	resp, err = http.Get(ts.URL + "/slug1//slug1.1")
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug1//slug1.1")

	resp, err = http.Get(joinPath(ts.URL, "slug2", "SOMETHING"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug2/SOMETHING")

	resp, err = http.Get(joinPath(ts.URL, "slug2", "SOMETHINGMORE/"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug2/SOMETHINGMORE/")

	resp, err = http.Get(joinPath(ts.URL, "slug2", "SOMETHING", "slug2.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /slug2/SOMETHING/slug2.1.1")

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /fallback-slash")

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash/"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /fallback-slash/")

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash", "failing"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /fallback-slash/")

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash2/"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /fallback-slash2/")

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash2"))
	if err != nil {
		t.Fatal(err)
	}
	testResp(t, resp, "GET /fallback-slash2/")

	resp, err = http.Get(joinPath(ts.URL, "slug2", "SOMETHING", "slug2.1.1", "slug2.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != handleAnyCode {
		t.Fatalf("expected %d, got %d", handleAnyCode, resp.StatusCode)
	}

	resp, err = http.Get(joinPath(ts.URL, "fallback-slash3/"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != handleAnyCode {
		t.Fatalf("expected %d, got %d", handleAnyCode, resp.StatusCode)
	}
}

func testResp(t *testing.T, resp *http.Response, wantBody string) {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: received status of %s", resp.Request.URL.Path, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	gotBody := string(body)
	if gotBody != wantBody {
		t.Fatalf("expected %s, got %s", wantBody, gotBody)
	}
}

func joinPath(base string, slugs ...string) string {
	url, err := urlpkg.Parse(base)
	if err != nil {
		panic("bad url: " + base)
	}
	url.Path = path.Join(append([]string{url.Path}, slugs...)...)
	if l := len(slugs); l != 0 {
		if sl := len(slugs[l-1]); sl != 0 && slugs[l-1][sl-1] == '/' {
			if pl := len(url.Path); pl != 0 && url.Path[pl-1] != '/' {
				url.Path += "/"
			}
		}
	}
	return url.String()
}
