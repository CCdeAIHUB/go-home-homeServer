using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Text.Json;
using Microsoft.UI.Xaml;

namespace GoHome.HomeServer.Windows;

public sealed partial class MainWindow : Window
{
    private readonly ObservableCollection<ClientRow> clients = new();
    private Process? process;

    public MainWindow()
    {
        InitializeComponent();
        ClientsList.ItemsSource = clients;
        ConnectHint.Text = "Windows 版用于连接公网服务器并查看运行统计。完整 LAN 转发仍建议部署 Linux/OpenWrt 家庭服务器。";
    }

    private void ConnectButton_Click(object sender, RoutedEventArgs e)
    {
        if (process is { HasExited: false })
        {
            return;
        }

        var server = NormalizeServer(ServerBox.Text.Trim());
        var authCode = AuthCodeBox.Password.Trim();
        if (server.Length == 0 || authCode.Length == 0)
        {
            ConnectHint.Text = "请填写公网服务器地址和授权码。";
            return;
        }

        var exe = Path.Combine(AppContext.BaseDirectory, "go-home-home-server.exe");
        if (!File.Exists(exe))
        {
            ConnectHint.Text = $"未找到 Go 核心程序：{exe}";
            return;
        }

        var args = $"-server {Quote(server)} -auth-code {Quote(authCode)}";
        if (!string.IsNullOrWhiteSpace(LanCidrBox.Text))
        {
            args += $" -lan-cidr {Quote(LanCidrBox.Text.Trim())}";
        }
        if (!string.IsNullOrWhiteSpace(LanInterfaceBox.Text))
        {
            args += $" -lan-interface {Quote(LanInterfaceBox.Text.Trim())}";
        }

        process = new Process
        {
            StartInfo = new ProcessStartInfo(exe, args)
            {
                UseShellExecute = false,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                CreateNoWindow = true,
            },
            EnableRaisingEvents = true,
        };
        process.OutputDataReceived += OnLogLine;
        process.ErrorDataReceived += OnLogLine;
        process.Exited += (_, _) => DispatcherQueue.TryEnqueue(() =>
        {
            LoadingRing.IsActive = false;
            ConnectPanel.Visibility = Visibility.Visible;
            StatsPanel.Visibility = Visibility.Collapsed;
            ConnectButton.IsEnabled = true;
            ConnectHint.Text = "连接已断开，可重新连接。";
        });

        try
        {
            process.Start();
            process.BeginOutputReadLine();
            process.BeginErrorReadLine();
        }
        catch (Exception ex)
        {
            ConnectHint.Text = $"启动失败：{ex.Message}";
            process = null;
            return;
        }

        LoadingRing.IsActive = true;
        ConnectButton.IsEnabled = false;
        ConnectHint.Text = "正在连接公网服务器...";
        StatusText.Text = "正在连接";
    }

    private void DisconnectButton_Click(object sender, RoutedEventArgs e)
    {
        StopProcess();
    }

    private void OnLogLine(object sender, DataReceivedEventArgs e)
    {
        if (string.IsNullOrWhiteSpace(e.Data))
        {
            return;
        }
        var line = e.Data;
        DispatcherQueue.TryEnqueue(() => HandleLogLine(line));
    }

    private void HandleLogLine(string line)
    {
        if (line.Contains("auth failed", StringComparison.OrdinalIgnoreCase))
        {
            ConnectHint.Text = "认证失败，请检查授权码。";
            StopProcess();
            return;
        }
        if (line.Contains("home-server device id", StringComparison.OrdinalIgnoreCase))
        {
            ConnectPanel.Visibility = Visibility.Collapsed;
            StatsPanel.Visibility = Visibility.Visible;
            LoadingRing.IsActive = false;
            StatusText.Text = "已连接公网服务器";
        }

        var marker = line.IndexOf("ui_stats ", StringComparison.Ordinal);
        if (marker < 0)
        {
            return;
        }
        var json = line[(marker + "ui_stats ".Length)..];
        try
        {
            var stats = JsonSerializer.Deserialize<ServiceStats>(json, JsonOptions);
            if (stats == null)
            {
                return;
            }
            ClientCountText.Text = stats.ConnectedClients.ToString();
            UpText.Text = FormatBytes(stats.Up);
            DownText.Text = FormatBytes(stats.Down);
            clients.Clear();
            foreach (var item in stats.Clients)
            {
                clients.Add(new ClientRow(
                    item.DeviceId,
                    string.IsNullOrWhiteSpace(item.Ip) ? "-" : item.Ip,
                    FormatBytes(item.Up),
                    FormatBytes(item.Down)));
            }
        }
        catch
        {
            // Ignore malformed log lines from older binaries.
        }
    }

    private void StopProcess()
    {
        if (process is { HasExited: false })
        {
            process.Kill(entireProcessTree: true);
        }
        process = null;
    }

    private static string NormalizeServer(string value)
    {
        if (value.StartsWith("http://", StringComparison.OrdinalIgnoreCase))
        {
            value = "ws://" + value["http://".Length..];
        }
        if (value.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
        {
            value = "wss://" + value["https://".Length..];
        }
        if (!value.StartsWith("ws://", StringComparison.OrdinalIgnoreCase) && !value.StartsWith("wss://", StringComparison.OrdinalIgnoreCase))
        {
            value = "ws://" + value;
        }
        if (!value.EndsWith("/ws", StringComparison.OrdinalIgnoreCase))
        {
            value = value.TrimEnd('/') + "/ws";
        }
        return value;
    }

    private static string Quote(string value) => "\"" + value.Replace("\"", "\\\"") + "\"";

    private static string FormatBytes(ulong value)
    {
        string[] units = ["B", "KB", "MB", "GB", "TB"];
        var size = (double)value;
        var index = 0;
        while (size >= 1024 && index < units.Length - 1)
        {
            size /= 1024;
            index++;
        }
        return index == 0 ? $"{value} B" : $"{size:0.##} {units[index]}";
    }

    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
    };
}

public sealed record ClientRow(string DeviceId, string Ip, string Up, string Down);

internal sealed class ServiceStats
{
    public int ConnectedClients { get; set; }
    public ulong Up { get; set; }
    public ulong Down { get; set; }
    public List<ClientStat> Clients { get; set; } = [];
}

internal sealed class ClientStat
{
    public string DeviceId { get; set; } = "";
    public string Ip { get; set; } = "";
    public ulong Up { get; set; }
    public ulong Down { get; set; }
}
