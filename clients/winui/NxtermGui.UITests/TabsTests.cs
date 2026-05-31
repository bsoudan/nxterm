using System;
using System.Linq;
using System.Threading;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using OpenQA.Selenium.Appium;
using OpenQA.Selenium.Appium.Windows;

namespace NxtermGui.UITests;

// Drives the WinUI 3 GUI client through WinAppDriver. The terminal viewport is
// Win2D-drawn (no UIA), so these tests cover the XAML chrome — the tab strip and
// status bar — and the connection handshake (status reaches "connected" only
// after the server replies). Each test launches a fresh, uniquely-named session
// so it starts with exactly one region.
[TestClass]
public class TabsTests
{
    private const string WinAppDriverUrl = "http://127.0.0.1:4723";
    private WindowsDriver<WindowsElement> _session = null!;

    [TestInitialize]
    public void Setup()
    {
        var appPath = Environment.GetEnvironmentVariable("NXTERMGUI_PATH")
            ?? throw new InvalidOperationException("NXTERMGUI_PATH must point to NxtermGui.exe");
        var endpoint = Environment.GetEnvironmentVariable("NXTERM_ENDPOINT") ?? "10.0.2.2:7654";
        var session = "uitest-" + Guid.NewGuid().ToString("N")[..8];

        var options = new AppiumOptions();
        options.AddAdditionalCapability("app", appPath);
        options.AddAdditionalCapability("appArguments", $"{endpoint} {session}");
        options.AddAdditionalCapability("deviceName", "WindowsPC");

        // WinUI 3 creates its window asynchronously; retry the cold start.
        OpenQA.Selenium.WebDriverException? last = null;
        for (var attempt = 0; attempt < 5; attempt++)
        {
            try
            {
                _session = new WindowsDriver<WindowsElement>(new Uri(WinAppDriverUrl), options);
                _session.Manage().Timeouts().ImplicitWait = TimeSpan.FromSeconds(2);
                _expectedSession = session;
                return;
            }
            catch (OpenQA.Selenium.WebDriverException ex)
            {
                last = ex;
                foreach (var p in System.Diagnostics.Process.GetProcessesByName("NxtermGui"))
                    try { p.Kill(); } catch { }
                Thread.Sleep(1000);
            }
        }
        throw last!;
    }

    [TestCleanup]
    public void Teardown() => _session?.Quit();

    private string _expectedSession = "";

    private IList<WindowsElement> Tabs()
        => _session.FindElementsByAccessibilityId("TerminalTab").ToList();
    private string Text(string id) => _session.FindElementByAccessibilityId(id).Text;
    private string ActiveRegion() => Text("ActiveRegionId");

    private void WaitUntil(Func<bool> cond, string what, int seconds = 20)
    {
        var end = DateTime.UtcNow.AddSeconds(seconds);
        while (DateTime.UtcNow < end)
        {
            try { if (cond()) return; } catch { /* elements settling */ }
            Thread.Sleep(300);
        }
        Assert.Fail($"timed out waiting for {what}");
    }

    [TestMethod]
    public void ConnectsAndShowsOneTab()
    {
        WaitUntil(() => Text("StatusRight").Contains("connected") && Tabs().Count >= 1, "connect");

        Assert.AreEqual(1, Tabs().Count, "fresh session should have exactly one region/tab");
        StringAssert.Contains(Text("StatusLeft"), $"{_expectedSession}@", "status bar shows session@endpoint");
        StringAssert.Contains(Text("StatusRight"), "connected");
        Assert.IsTrue(ActiveRegion().Length > 0, "an active region should be reported");
    }

    [TestMethod]
    public void NewTabSwitchAndClose()
    {
        WaitUntil(() => Text("StatusRight").Contains("connected") && Tabs().Count == 1, "connect");
        var first = ActiveRegion();

        // New tab: count goes to 2 and the new region becomes active.
        _session.FindElementByAccessibilityId("NewTabButton").Click();
        WaitUntil(() => Tabs().Count == 2, "second tab to appear");
        WaitUntil(() => ActiveRegion() != first && ActiveRegion().Length > 0, "new tab to become active");
        var second = ActiveRegion();
        Assert.AreNotEqual(first, second);

        // Switch back to the first tab: active region returns to the first.
        Tabs()[0].Click();
        WaitUntil(() => ActiveRegion() == first, "switch back to first tab");

        // Close the second tab: count returns to 1.
        Tabs()[1].FindElement(OpenQA.Selenium.Appium.MobileBy.AccessibilityId("CloseTab")).Click();
        WaitUntil(() => Tabs().Count == 1, "second tab to close");
    }
}
