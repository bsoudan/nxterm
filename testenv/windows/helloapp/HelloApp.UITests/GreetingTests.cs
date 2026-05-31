using System;
using System.Diagnostics;
using System.Threading;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using OpenQA.Selenium.Appium;
using OpenQA.Selenium.Appium.Windows;

namespace HelloApp.UITests;

[TestClass]
public class GreetingTests
{
    // WinAppDriver listens here by default once started on the interactive desktop.
    private const string WinAppDriverUrl = "http://127.0.0.1:4723";

    private WindowsDriver<WindowsElement> _session = null!;

    [TestInitialize]
    public void Setup()
    {
        var appPath = Environment.GetEnvironmentVariable("HELLOAPP_PATH")
            ?? throw new InvalidOperationException(
                "HELLOAPP_PATH must point to the published HelloApp.exe");

        var options = new AppiumOptions();
        options.AddAdditionalCapability("app", appPath);
        options.AddAdditionalCapability("deviceName", "WindowsPC");

        // A WinUI 3 app creates its main window asynchronously once the Windows
        // App SDK runtime has bootstrapped, which on a cold start can outlast
        // WinAppDriver's window-find timeout ("Failed to locate opened
        // application window"). Retry session creation, killing the orphaned
        // launch each time so attempts don't pile up.
        OpenQA.Selenium.WebDriverException? last = null;
        for (var attempt = 0; attempt < 5; attempt++)
        {
            try
            {
                _session = new WindowsDriver<WindowsElement>(new Uri(WinAppDriverUrl), options);
                _session.Manage().Timeouts().ImplicitWait = TimeSpan.FromSeconds(5);
                return;
            }
            catch (OpenQA.Selenium.WebDriverException ex)
            {
                last = ex;
                KillHelloApp();
                Thread.Sleep(1000);
            }
        }
        throw last!;
    }

    private static void KillHelloApp()
    {
        foreach (var p in Process.GetProcessesByName("HelloApp"))
        {
            try { p.Kill(); p.WaitForExit(2000); } catch { /* already gone */ }
        }
    }

    [TestCleanup]
    public void Teardown()
    {
        _session?.Quit();
    }

    [TestMethod]
    public void GreetButtonShowsGreeting()
    {
        _session.FindElementByAccessibilityId("NameBox").SendKeys("Bill");
        _session.FindElementByAccessibilityId("GreetButton").Click();

        var greeting = _session.FindElementByAccessibilityId("GreetingText");
        Assert.AreEqual("hello, Bill", greeting.Text);
    }

    [TestMethod]
    public void EnterKeyShowsGreeting()
    {
        var nameBox = _session.FindElementByAccessibilityId("NameBox");
        nameBox.SendKeys("Ada");
        nameBox.SendKeys(OpenQA.Selenium.Keys.Enter);

        var greeting = _session.FindElementByAccessibilityId("GreetingText");
        Assert.AreEqual("hello, Ada", greeting.Text);
    }
}
