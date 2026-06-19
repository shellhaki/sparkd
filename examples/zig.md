# Zig

Using `curl` from Zig keeps the example dependency-free:

```zig
const std = @import("std");

pub fn main() !void {
    var child = std.process.Child.init(&.{
        "curl",
        "-X", "POST",
        "http://server-ip:8721/create",
        "-H", "content-type: application/json",
        "-d", "{\"name\":\"zig-cell\",\"dbtype\":\"pg\",\"port\":3001,\"ram\":\"128mb\",\"disk\":\"512mb\"}",
    }, std.heap.page_allocator);

    child.stdout_behavior = .Inherit;
    child.stderr_behavior = .Inherit;
    const result = try child.spawnAndWait();
    std.debug.print("curl exited with {any}\n", .{result});
}
```
