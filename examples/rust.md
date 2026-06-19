# Rust

Using `reqwest`:

```rust
use reqwest::blocking::Client;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = Client::new();
    let response = client
        .post("http://server-ip:8721/create")
        .json(&serde_json::json!({
            "name": "rust-cell",
            "dbtype": "pg",
            "port": 3001,
            "ram": "128mb",
            "disk": "512mb"
        }))
        .send()?;

    println!("{}", response.status());
    println!("{}", response.text()?);
    Ok(())
}
```
