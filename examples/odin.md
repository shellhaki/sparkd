# Odin

Using `curl` from Odin keeps the example focused on the SparkD request:

```odin
package main

import "core:os/os2"

main :: proc() {
    args := []string{
        "curl",
        "-X", "POST",
        "http://server-ip:8721/create",
        "-H", "content-type: application/json",
        "-d", `{"name":"odin-cell","dbtype":"pg","port":3001,"ram":"128mb","disk":"512mb"}`,
    }

    os2.exec(args)
}
```
